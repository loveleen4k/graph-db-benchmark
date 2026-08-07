package db

import (
	"context"
	"fmt"
	"log"
	"strings"

	driver "github.com/arangodb/go-driver"
	"github.com/arangodb/go-driver/http"
)

const (
	arangoDBName     = "graph_benchmark"
	arangoGraphName  = "social"
	arangoPersonCol  = "Person"
	arangoFollowsCol = "FOLLOWS"
)

// ArangoDB implements GraphDB with AQL (not Cypher). Semantics still match the
// Bolt backends: Person documents keyed by "id", FOLLOWS edge collection, and
// the same workload shapes (batch load, k-hop traversal, point/index lookup,
// count aggregation, single write) so cross-database timing stays comparable.
type ArangoDB struct {
	uri      string
	username string
	password string
	client   driver.Client
	db       driver.Database
}

// NewArangoDB returns an ArangoDB client configured from environment values.
func NewArangoDB(uri, username, password string) *ArangoDB {
	return &ArangoDB{
		uri:      uri,
		username: username,
		password: password,
	}
}

func (a *ArangoDB) Name() string { return "arangodb" }

func (a *ArangoDB) Connect(ctx context.Context) error {
	conn, err := http.NewConnection(http.ConnectionConfig{
		Endpoints: []string{a.uri},
	})
	if err != nil {
		return fmt.Errorf("arangodb: connection: %w", err)
	}
	client, err := driver.NewClient(driver.ClientConfig{
		Connection:     conn,
		Authentication: driver.BasicAuthentication(a.username, a.password),
	})
	if err != nil {
		return fmt.Errorf("arangodb: client: %w", err)
	}
	a.client = client

	exists, err := client.DatabaseExists(ctx, arangoDBName)
	if err != nil {
		return fmt.Errorf("arangodb: DatabaseExists: %w", err)
	}
	if exists {
		a.db, err = client.Database(ctx, arangoDBName)
	} else {
		a.db, err = client.CreateDatabase(ctx, arangoDBName, nil)
	}
	if err != nil {
		return fmt.Errorf("arangodb: open database: %w", err)
	}
	return a.Ping(ctx)
}

func (a *ArangoDB) Close(ctx context.Context) error {
	// HTTP client has no explicit Close; drop references.
	a.db = nil
	a.client = nil
	return nil
}

func (a *ArangoDB) Ping(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("arangodb: not connected")
	}
	_, err := a.client.Version(ctx)
	return err
}

// CreateSchema ensures Person + FOLLOWS collections, a named graph, and an
// index on Person.name (document _key / id used for point lookups).
//
// Measures: DDL / collection+index setup cost.
func (a *ArangoDB) CreateSchema(ctx context.Context) error {
	if a.db == nil {
		return fmt.Errorf("arangodb: not connected")
	}

	if err := a.ensureCollection(ctx, arangoPersonCol, false); err != nil {
		return err
	}
	if err := a.ensureCollection(ctx, arangoFollowsCol, true); err != nil {
		return err
	}

	// Persistent index on name for IndexedLookup (AQL FILTER equivalence).
	person, err := a.db.Collection(ctx, arangoPersonCol)
	if err != nil {
		return fmt.Errorf("arangodb: person collection: %w", err)
	}
	_, _, err = person.EnsurePersistentIndex(ctx, []string{"name"}, &driver.EnsurePersistentIndexOptions{
		Name:   "idx_person_name",
		Unique: false,
	})
	if err != nil {
		return fmt.Errorf("arangodb: name index: %w", err)
	}

	// Unique (_from,_to) index makes edge UPSERT MERGE-equivalent and blocks
	// duplicate FOLLOWS pairs if a reload races or omits deterministic _key.
	follows, err := a.db.Collection(ctx, arangoFollowsCol)
	if err != nil {
		return fmt.Errorf("arangodb: follows collection: %w", err)
	}
	_, _, err = follows.EnsurePersistentIndex(ctx, []string{"_from", "_to"}, &driver.EnsurePersistentIndexOptions{
		Name:   "idx_follows_from_to",
		Unique: true,
	})
	if err != nil {
		return fmt.Errorf("arangodb: follows from/to unique index: %w", err)
	}

	exists, err := a.db.GraphExists(ctx, arangoGraphName)
	if err != nil {
		return fmt.Errorf("arangodb: GraphExists: %w", err)
	}
	if !exists {
		_, err = a.db.CreateGraph(ctx, arangoGraphName, &driver.CreateGraphOptions{
			EdgeDefinitions: []driver.EdgeDefinition{
				{
					Collection: arangoFollowsCol,
					From:       []string{arangoPersonCol},
					To:         []string{arangoPersonCol},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("arangodb: CreateGraph: %w", err)
		}
	}
	return nil
}

// LoadNodesBatch upserts Person documents keyed by id (_key = id).
//
// Measures: batched vertex ingest throughput (AQL UPSERT).
func (a *ArangoDB) LoadNodesBatch(ctx context.Context, nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}
	batch := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		doc := cloneProps(n.Properties)
		doc["_key"] = n.ID
		doc["id"] = n.ID
		batch = append(batch, doc)
	}

	// UPSERT mirrors Cypher MERGE on Person.id.
	const aql = `
FOR row IN @batch
  UPSERT { _key: row._key }
  INSERT row
  UPDATE row
  IN Person`

	_, err := a.db.Query(ctx, aql, map[string]any{"batch": batch})
	if err != nil {
		return fmt.Errorf("arangodb: LoadNodesBatch: %w", err)
	}
	return nil
}

// LoadRelationshipsBatch upserts FOLLOWS edges between Person documents.
//
// Measures: batched edge ingest throughput after vertices exist.
//
// DOCUMENT()+FILTER mirrors Cypher MATCH on endpoints: missing vertices are
// skipped (no dangling edges) rather than inserted. UPSERT on (_from,_to)
// mirrors Cypher MERGE (a)-[:FOLLOWS]->(b) so reloads are idempotent; a plain
// INSERT without a stable key would create duplicate/auto-keyed edges on retry.
// Returns written count so callers can accumulate skipped_edges = submitted - written.
func (a *ArangoDB) LoadRelationshipsBatch(ctx context.Context, rels []Relationship) (int, error) {
	if len(rels) == 0 {
		return 0, nil
	}
	batch := make([]map[string]any, 0, len(rels))
	for _, r := range rels {
		doc := cloneProps(r.Properties)
		doc["_from"] = arangoPersonCol + "/" + r.FromID
		doc["_to"] = arangoPersonCol + "/" + r.ToID
		// Deterministic _key so overwrite/UPSERT is stable across reloads.
		// Prefer explicit relationship ID when present; otherwise from_to.
		if r.ID != "" {
			doc["_key"] = r.ID
		} else {
			doc["_key"] = r.FromID + "_" + r.ToID
		}
		batch = append(batch, doc)
	}

	// FILTER drops rows whose endpoints are missing (MATCH-equivalent skip).
	// UPSERT matches on directed endpoints (not only _key) so leftover
	// auto-keyed edges from older loads are updated in place instead of
	// accumulating a second FOLLOWS document for the same pair.
	// UPDATE must not touch immutable edge identity fields (_key/_from/_to).
	const aql = `
FOR row IN @batch
  LET fromDoc = DOCUMENT(row._from)
  LET toDoc = DOCUMENT(row._to)
  FILTER fromDoc != null AND toDoc != null
  LET patch = UNSET(row, '_key', '_id', '_rev', '_from', '_to')
  UPSERT { _from: row._from, _to: row._to }
  INSERT row
  UPDATE patch
  IN FOLLOWS
  COLLECT WITH COUNT INTO created
  RETURN created`

	cursor, err := a.db.Query(ctx, aql, map[string]any{"batch": batch})
	if err != nil {
		return 0, fmt.Errorf("arangodb: LoadRelationshipsBatch: %w", err)
	}
	defer cursor.Close()

	var written int
	if _, err := cursor.ReadDocument(ctx, &written); driver.IsNoMoreDocuments(err) {
		written = 0
	} else if err != nil {
		return 0, fmt.Errorf("arangodb: LoadRelationshipsBatch read: %w", err)
	}

	if skipped := len(batch) - written; skipped > 0 {
		log.Printf("warning: arangodb: LoadRelationshipsBatch skipped %d of %d edges (delta=%d; missing DOCUMENT endpoints)",
			skipped, len(batch), skipped)
	}
	return written, nil
}

// Traversal walks the social graph from startID for up to hops hops.
//
// N-hop = count of distinct nodes reachable within 1..N hops outward from the
// start node, excluding the start node itself, following FOLLOWS outbound
// (matches bolt_common.go directed Cypher). uniqueVertices:"global" +
// COLLECT WITH COUNT dedupes multi-path hits.
//
// Measures: multi-hop neighborhood expansion (AQL graph traversal).
func (a *ArangoDB) Traversal(ctx context.Context, startID string, hops int) (int, error) {
	// Same allowlist as BoltGraphDB even though hops is a bind var here.
	if hops < 1 || hops > 3 {
		return 0, fmt.Errorf("arangodb: Traversal: hops must be in 1..3, got %d", hops)
	}

	// Depth bound 1..@hops excludes depth 0 structurally: the start vertex is
	// never emitted by AQL traversal (same effect as Cypher WHERE m <> s).
	// FILTER v._key != @startKey is defensive parity with that Cypher clause.
	// order:bfs is required by Arango when uniqueVertices is "global".
	// OUTBOUND = directed FOLLOWS like Cypher -[:FOLLOWS]-> .
	const aql = `
FOR v IN 1..@hops OUTBOUND @start GRAPH social
  OPTIONS { uniqueVertices: "global", order: "bfs" }
  FILTER v._key != @startKey
  COLLECT WITH COUNT INTO cnt
  RETURN cnt`

	start := arangoPersonCol + "/" + startID
	cursor, err := a.db.Query(ctx, aql, map[string]any{
		"hops":     hops,
		"start":    start,
		"startKey": startID,
	})
	if err != nil {
		return 0, fmt.Errorf("arangodb: Traversal: %w", err)
	}
	defer cursor.Close()

	var cnt int
	if _, err := cursor.ReadDocument(ctx, &cnt); driver.IsNoMoreDocuments(err) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("arangodb: Traversal read: %w", err)
	}
	return cnt, nil
}

// PointLookup fetches one Person by primary key id (_key).
//
// Measures: primary-key document get latency.
func (a *ArangoDB) PointLookup(ctx context.Context, id string) (bool, error) {
	const aql = `
FOR p IN Person
  FILTER p._key == @id
  LIMIT 1
  RETURN p._key`

	cursor, err := a.db.Query(ctx, aql, map[string]any{"id": id})
	if err != nil {
		return false, fmt.Errorf("arangodb: PointLookup: %w", err)
	}
	defer cursor.Close()

	var key string
	_, err = cursor.ReadDocument(ctx, &key)
	if driver.IsNoMoreDocuments(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("arangodb: PointLookup read: %w", err)
	}
	return true, nil
}

// IndexedLookup counts Person documents where property == value.
// Benchmark indexed property is "name".
//
// Measures: secondary-index equality lookup (persistent index on name).
func (a *ArangoDB) IndexedLookup(ctx context.Context, property, value string) (int, error) {
	// Bind property name via template-safe allowlist: only "name" in methodology,
	// but still pass value as a bind var. Dynamic attribute access uses AQL [].
	const aql = `
FOR p IN Person
  FILTER p[@property] == @value
  COLLECT WITH COUNT INTO cnt
  RETURN cnt`

	cursor, err := a.db.Query(ctx, aql, map[string]any{
		"property": property,
		"value":    value,
	})
	if err != nil {
		return 0, fmt.Errorf("arangodb: IndexedLookup: %w", err)
	}
	defer cursor.Close()

	var cnt int
	if _, err := cursor.ReadDocument(ctx, &cnt); driver.IsNoMoreDocuments(err) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("arangodb: IndexedLookup read: %w", err)
	}
	return cnt, nil
}

// Aggregation returns the number of Person documents.
//
// Measures: collection count / aggregation over the vertex set.
func (a *ArangoDB) Aggregation(ctx context.Context) (int64, error) {
	const aql = `
RETURN LENGTH(Person)`

	cursor, err := a.db.Query(ctx, aql, nil)
	if err != nil {
		return 0, fmt.Errorf("arangodb: Aggregation: %w", err)
	}
	defer cursor.Close()

	var cnt int64
	if _, err := cursor.ReadDocument(ctx, &cnt); err != nil {
		return 0, fmt.Errorf("arangodb: Aggregation read: %w", err)
	}
	return cnt, nil
}

// WriteOne upserts a single Person document.
//
// Measures: single-document write latency (OLTP-style).
func (a *ArangoDB) WriteOne(ctx context.Context, n Node) error {
	doc := cloneProps(n.Properties)
	doc["_key"] = n.ID
	doc["id"] = n.ID

	const aql = `
UPSERT { _key: @key }
INSERT @doc
UPDATE @doc
IN Person`

	_, err := a.db.Query(ctx, aql, map[string]any{
		"key": n.ID,
		"doc": doc,
	})
	if err != nil {
		return fmt.Errorf("arangodb: WriteOne: %w", err)
	}
	return nil
}

// CleanupSmoketest removes Person docs (and incident FOLLOWS edges) whose
// _key starts with "smoketest-".
func (a *ArangoDB) CleanupSmoketest(ctx context.Context) error {
	if a.db == nil {
		return fmt.Errorf("arangodb: not connected")
	}
	// ignoreErrors avoids hard-fail when an edge/vertex was already removed.
	const aql = `
FOR e IN FOLLOWS
  FILTER STARTS_WITH(PARSE_IDENTIFIER(e._from).key, @prefix)
      OR STARTS_WITH(PARSE_IDENTIFIER(e._to).key, @prefix)
  REMOVE e IN FOLLOWS OPTIONS { ignoreErrors: true }
FOR p IN Person
  FILTER STARTS_WITH(p._key, @prefix)
  REMOVE p IN Person OPTIONS { ignoreErrors: true }
RETURN 1`

	cursor, err := a.db.Query(ctx, aql, map[string]any{"prefix": "smoketest-"})
	if err != nil {
		return fmt.Errorf("arangodb: CleanupSmoketest: %w", err)
	}
	defer cursor.Close()
	for {
		var ignored any
		_, err := cursor.ReadDocument(ctx, &ignored)
		if driver.IsNoMoreDocuments(err) {
			break
		}
		if err != nil {
			return fmt.Errorf("arangodb: CleanupSmoketest drain: %w", err)
		}
	}
	return nil
}

// ClearBenchmarkData truncates Person and FOLLOWS so a failed/partial load
// does not leave stale rows before a fair reload.
func (a *ArangoDB) ClearBenchmarkData(ctx context.Context) (removed int64, err error) {
	if a.db == nil {
		return 0, fmt.Errorf("arangodb: not connected")
	}
	before, err := a.Aggregation(ctx)
	if err != nil {
		// Collections may not exist yet on a fresh instance.
		if strings.Contains(strings.ToLower(err.Error()), "not found") ||
			strings.Contains(strings.ToLower(err.Error()), "unknown collection") {
			return 0, nil
		}
		return 0, fmt.Errorf("arangodb: ClearBenchmarkData count: %w", err)
	}

	for _, name := range []string{arangoFollowsCol, arangoPersonCol} {
		exists, err := a.db.CollectionExists(ctx, name)
		if err != nil {
			return 0, fmt.Errorf("arangodb: CollectionExists(%s): %w", name, err)
		}
		if !exists {
			continue
		}
		col, err := a.db.Collection(ctx, name)
		if err != nil {
			return 0, fmt.Errorf("arangodb: Collection(%s): %w", name, err)
		}
		if err := col.Truncate(ctx); err != nil {
			return 0, fmt.Errorf("arangodb: Truncate(%s): %w", name, err)
		}
	}
	return before, nil
}

func (a *ArangoDB) ensureCollection(ctx context.Context, name string, edge bool) error {
	exists, err := a.db.CollectionExists(ctx, name)
	if err != nil {
		return fmt.Errorf("arangodb: CollectionExists(%s): %w", name, err)
	}
	if exists {
		return nil
	}
	var opts *driver.CreateCollectionOptions
	if edge {
		opts = &driver.CreateCollectionOptions{Type: driver.CollectionTypeEdge}
	}
	_, err = a.db.CreateCollection(ctx, name, opts)
	if err != nil {
		return fmt.Errorf("arangodb: CreateCollection(%s): %w", name, err)
	}
	return nil
}
