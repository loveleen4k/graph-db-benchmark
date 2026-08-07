package db

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	falkordb "github.com/FalkorDB/falkordb-go/v2"
)

const falkorGraphName = "graph_benchmark"

// FalkorDB implements GraphDB with the official FalkorDB Go client (Redis/
// RESP protocol + GRAPH.QUERY), not Bolt. Cypher query text matches
// bolt_common.go so methodology stays identical across Cypher backends.
//
// Quirk: connection is host:port (+ optional user/password), typically via
// falkor:// or redis:// URIs — never bolt://.
type FalkorDB struct {
	uri      string
	username string
	password string
	client   *falkordb.FalkorDB
	graph    *falkordb.Graph
}

// NewFalkorDB returns a FalkorDB client configured from environment values.
func NewFalkorDB(uri, username, password string) *FalkorDB {
	return &FalkorDB{
		uri:      uri,
		username: username,
		password: password,
	}
}

func (f *FalkorDB) Name() string { return "falkordb" }

func (f *FalkorDB) Connect(ctx context.Context) error {
	client, err := dialFalkor(f.uri, f.username, f.password)
	if err != nil {
		return fmt.Errorf("falkordb: connect: %w", err)
	}
	f.client = client
	f.graph = client.SelectGraph(falkorGraphName)
	if err := f.Ping(ctx); err != nil {
		_ = f.Close(ctx)
		return err
	}
	return nil
}

func (f *FalkorDB) Close(ctx context.Context) error {
	if f.client == nil {
		return nil
	}
	err := f.client.Conn.Close()
	f.client = nil
	f.graph = nil
	return err
}

func (f *FalkorDB) Ping(ctx context.Context) error {
	if f.client == nil {
		return fmt.Errorf("falkordb: not connected")
	}
	return f.client.Conn.Ping(ctx).Err()
}

// CreateSchema installs Person.id uniqueness support and a Person.name index.
//
// FalkorDB does not accept Neo4j 5 "CREATE CONSTRAINT ... IF NOT EXISTS FOR
// ... REQUIRE" DDL. Workload Cypher below still matches bolt_common.go exactly;
// only this DDL uses FalkorDB-native forms:
//   CREATE INDEX FOR (p:Person) ON (p.id|p.name)
//   GRAPH.CONSTRAINT CREATE ... UNIQUE NODE Person PROPERTIES 1 id
func (f *FalkorDB) CreateSchema(ctx context.Context) error {
	for _, cypher := range []string{
		`CREATE INDEX FOR (p:Person) ON (p.id)`,
		`CREATE INDEX FOR (p:Person) ON (p.name)`,
	} {
		if err := f.run(cypher, nil); err != nil && !isFalkorAlreadyExists(err) {
			return fmt.Errorf("falkordb: create index: %w", err)
		}
	}

	// Unique constraint requires the supporting index above (async on server).
	err := f.client.Conn.Do(ctx, "GRAPH.CONSTRAINT", "CREATE", falkorGraphName,
		"UNIQUE", "NODE", "Person", "PROPERTIES", 1, "id").Err()
	if err != nil && !isFalkorAlreadyExists(err) {
		return fmt.Errorf("falkordb: create id constraint: %w", err)
	}
	return nil
}

// LoadNodesBatch — same Cypher as bolt_common.go (UNWIND + MERGE).
func (f *FalkorDB) LoadNodesBatch(ctx context.Context, nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}
	batch := make([]interface{}, 0, len(nodes))
	for _, n := range nodes {
		props := clonePropsIface(n.Properties)
		props["id"] = n.ID
		batch = append(batch, map[string]interface{}{
			"id":    n.ID,
			"props": props,
		})
	}

	const cypher = `
UNWIND $batch AS row
MERGE (n:Person {id: row.id})
SET n += row.props`

	if err := f.run(cypher, map[string]interface{}{"batch": batch}); err != nil {
		return fmt.Errorf("falkordb: LoadNodesBatch: %w", err)
	}
	return nil
}

// LoadRelationshipsBatch — same Cypher as bolt_common.go (MATCH endpoints +
// MERGE edge + RETURN count(rel)).
func (f *FalkorDB) LoadRelationshipsBatch(ctx context.Context, rels []Relationship) (int, error) {
	if len(rels) == 0 {
		return 0, nil
	}
	batch := make([]interface{}, 0, len(rels))
	for _, r := range rels {
		batch = append(batch, map[string]interface{}{
			"from":  r.FromID,
			"to":    r.ToID,
			"props": clonePropsIface(r.Properties),
		})
	}

	const cypher = `
UNWIND $batch AS row
MATCH (a:Person {id: row.from})
MATCH (b:Person {id: row.to})
MERGE (a)-[rel:FOLLOWS]->(b)
SET rel += row.props
RETURN count(rel) AS created`

	val, err := f.runSingle(cypher, map[string]interface{}{"batch": batch})
	if err != nil {
		return 0, fmt.Errorf("falkordb: LoadRelationshipsBatch: %w", err)
	}
	written := toInt(val)
	if skipped := len(batch) - written; skipped > 0 {
		log.Printf("warning: falkordb: LoadRelationshipsBatch skipped %d of %d edges (delta=%d; missing MATCH endpoints)",
			skipped, len(batch), skipped)
	}
	return written, nil
}

// Traversal — same Cypher as bolt_common.go ([*1..N], count DISTINCT).
func (f *FalkorDB) Traversal(ctx context.Context, startID string, hops int) (int, error) {
	if hops < 1 || hops > 3 {
		return 0, fmt.Errorf("falkordb: Traversal: hops must be in 1..3, got %d", hops)
	}

	cypher := fmt.Sprintf(`
MATCH (s:Person {id: $startID})-[*1..%d]-(m)
WHERE m <> s
RETURN count(DISTINCT m) AS cnt`, hops)

	val, err := f.runSingle(cypher, map[string]interface{}{"startID": startID})
	if err != nil {
		return 0, fmt.Errorf("falkordb: Traversal: %w", err)
	}
	return toInt(val), nil
}

// PointLookup — same Cypher as bolt_common.go.
func (f *FalkorDB) PointLookup(ctx context.Context, id string) (bool, error) {
	const cypher = `
MATCH (n:Person {id: $id})
RETURN n.id AS id
LIMIT 1`

	res, err := f.graph.Query(cypher, map[string]interface{}{"id": id}, nil)
	if err != nil {
		return false, fmt.Errorf("falkordb: PointLookup: %w", err)
	}
	return res.Next(), nil
}

// IndexedLookup — same Cypher as bolt_common.go.
func (f *FalkorDB) IndexedLookup(ctx context.Context, property, value string) (int, error) {
	const cypher = `
MATCH (n:Person)
WHERE n[$property] = $value
RETURN count(n) AS cnt`

	val, err := f.runSingle(cypher, map[string]interface{}{
		"property": property,
		"value":    value,
	})
	if err != nil {
		return 0, fmt.Errorf("falkordb: IndexedLookup: %w", err)
	}
	return toInt(val), nil
}

// Aggregation — same Cypher as bolt_common.go.
func (f *FalkorDB) Aggregation(ctx context.Context) (int64, error) {
	const cypher = `
MATCH (n:Person)
RETURN count(n) AS cnt`

	val, err := f.runSingle(cypher, nil)
	if err != nil {
		return 0, fmt.Errorf("falkordb: Aggregation: %w", err)
	}
	return toInt64(val), nil
}

// WriteOne — same Cypher as bolt_common.go.
func (f *FalkorDB) WriteOne(ctx context.Context, n Node) error {
	props := clonePropsIface(n.Properties)
	props["id"] = n.ID

	const cypher = `
MERGE (p:Person {id: $id})
SET p += $props`

	if err := f.run(cypher, map[string]interface{}{
		"id":    n.ID,
		"props": props,
	}); err != nil {
		return fmt.Errorf("falkordb: WriteOne: %w", err)
	}
	return nil
}

// CleanupSmoketest — same Cypher as bolt_common.go.
func (f *FalkorDB) CleanupSmoketest(ctx context.Context) error {
	const cypher = `
MATCH (n:Person)
WHERE n.id STARTS WITH $prefix
DETACH DELETE n`
	if err := f.run(cypher, map[string]interface{}{"prefix": "smoketest-"}); err != nil {
		return fmt.Errorf("falkordb: CleanupSmoketest: %w", err)
	}
	return nil
}

func (f *FalkorDB) run(cypher string, params map[string]interface{}) error {
	if f.graph == nil {
		return fmt.Errorf("falkordb: not connected")
	}
	_, err := f.graph.Query(cypher, params, nil)
	return err
}

func (f *FalkorDB) runSingle(cypher string, params map[string]interface{}) (any, error) {
	if f.graph == nil {
		return nil, fmt.Errorf("falkordb: not connected")
	}
	res, err := f.graph.Query(cypher, params, nil)
	if err != nil {
		return nil, err
	}
	if !res.Next() {
		return int64(0), nil
	}
	val, err := res.Record().GetByIndex(0)
	if err != nil {
		return nil, err
	}
	return val, nil
}

func dialFalkor(uri, username, password string) (*falkordb.FalkorDB, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil, fmt.Errorf("empty FALKORDB_URI")
	}

	// Bare host:port (no scheme).
	if !strings.Contains(uri, "://") {
		return falkordb.FalkorDBNew(&falkordb.ConnectionOption{
			Addr:     uri,
			Username: username,
			Password: password,
		})
	}

	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(u.Scheme) {
	case "falkor", "falkors", "redis", "rediss":
		// ok
	default:
		return nil, fmt.Errorf("unsupported FalkorDB URI scheme %q (use falkor://, redis://, or host:port)", u.Scheme)
	}

	user := username
	if user == "" && u.User != nil {
		user = u.User.Username()
	}
	if u.Host == "" {
		return nil, fmt.Errorf("FALKORDB_URI missing host")
	}

	return falkordb.FalkorDBNew(&falkordb.ConnectionOption{
		Addr:     u.Host,
		Username: user,
		Password: password,
	})
}

func clonePropsIface(in map[string]any) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isFalkorAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already indexed") ||
		strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "index already exists") ||
		strings.Contains(msg, "constraint already exists")
}
