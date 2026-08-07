package db

import (
	"context"
	"fmt"
	"log"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// BoltGraphDB is a shared Cypher-over-Bolt implementation used by CognoDB,
// Neo4j, and Memgraph. FalkorDB uses its own RESP client in falkordb.go.
// All queries are parameterized; the only non-parameter substitution is the
// integer hop bound in Traversal, because Cypher does not allow parameters
// inside variable-length path ranges.
type BoltGraphDB struct {
	name     string
	uri      string
	username string
	password string
	driver   neo4j.DriverWithContext
}

// NewBoltGraphDB constructs an unconnected Bolt-backed GraphDB.
func NewBoltGraphDB(name, uri, username, password string) *BoltGraphDB {
	return &BoltGraphDB{
		name:     name,
		uri:      uri,
		username: username,
		password: password,
	}
}

func (b *BoltGraphDB) Name() string { return b.name }

func (b *BoltGraphDB) Connect(ctx context.Context) error {
	driver, err := neo4j.NewDriverWithContext(
		b.uri,
		neo4j.BasicAuth(b.username, b.password, ""),
	)
	if err != nil {
		return fmt.Errorf("%s: create driver: %w", b.name, err)
	}
	b.driver = driver
	if err := b.Ping(ctx); err != nil {
		_ = driver.Close(ctx)
		b.driver = nil
		return err
	}
	return nil
}

func (b *BoltGraphDB) Close(ctx context.Context) error {
	if b.driver == nil {
		return nil
	}
	err := b.driver.Close(ctx)
	b.driver = nil
	return err
}

// CleanupSmokeNodes DETACH DELETEs Person nodes with the given ids.
// Intended for throwaway smoke tests only.
func (b *BoltGraphDB) CleanupSmokeNodes(ctx context.Context, ids []string) error {
	const cypher = `
UNWIND $ids AS id
MATCH (n:Person {id: id})
DETACH DELETE n`
	_, err := b.runWrite(ctx, cypher, map[string]any{"ids": ids})
	if err != nil {
		return fmt.Errorf("%s: CleanupSmokeNodes: %w", b.name, err)
	}
	return nil
}

// CleanupSmoketest removes all Person nodes whose id starts with "smoketest-"
// (and their relationships via DETACH DELETE).
func (b *BoltGraphDB) CleanupSmoketest(ctx context.Context) error {
	const cypher = `
MATCH (n:Person)
WHERE n.id STARTS WITH $prefix
DETACH DELETE n`
	_, err := b.runWrite(ctx, cypher, map[string]any{"prefix": "smoketest-"})
	if err != nil {
		return fmt.Errorf("%s: CleanupSmoketest: %w", b.name, err)
	}
	return nil
}

// ClearBenchmarkData removes every Person node (and incident relationships)
// in batches so partial/failed loads can be wiped before a fair reload.
func (b *BoltGraphDB) ClearBenchmarkData(ctx context.Context) (removed int64, err error) {
	const cypher = `
MATCH (n:Person)
WITH n LIMIT 10000
DETACH DELETE n
RETURN count(*) AS c`
	for {
		val, err := b.runWriteSingle(ctx, cypher, nil)
		if err != nil {
			return removed, fmt.Errorf("%s: ClearBenchmarkData: %w", b.name, err)
		}
		n := toInt64(val)
		removed += n
		if n == 0 {
			return removed, nil
		}
	}
}

func (b *BoltGraphDB) Ping(ctx context.Context) error {
	if b.driver == nil {
		return fmt.Errorf("%s: not connected", b.name)
	}
	return b.driver.VerifyConnectivity(ctx)
}

// CreateSchema installs the benchmark Person/FOLLOWS shape:
// unique Person.id and an index on Person.name for IndexedLookup.
//
// Measures: DDL / constraint+index creation cost (not a query latency op).
func (b *BoltGraphDB) CreateSchema(ctx context.Context) error {
	// Uniqueness on id so MERGE/point lookup have a stable key.
	const constraintCypher = `
CREATE CONSTRAINT person_id IF NOT EXISTS
FOR (p:Person) REQUIRE p.id IS UNIQUE`

	// Secondary index on name for the IndexedLookup workload.
	const indexCypher = `
CREATE INDEX person_name IF NOT EXISTS
FOR (p:Person) ON (p.name)`

	if _, err := b.runWrite(ctx, constraintCypher, nil); err != nil {
		return fmt.Errorf("%s: create id constraint: %w", b.name, err)
	}
	if _, err := b.runWrite(ctx, indexCypher, nil); err != nil {
		return fmt.Errorf("%s: create name index: %w", b.name, err)
	}
	return nil
}

// LoadNodesBatch bulk-upserts Person nodes.
//
// Measures: batched write throughput for vertex ingest (UNWIND + MERGE).
//
// Write strategy: MERGE (not CREATE). MERGE is idempotent and safe to retry
// without duplicating vertices, which keeps load runs comparable across
// backends; tradeoff is higher per-row cost than CREATE (which is faster but
// duplicates on retry and skews throughput).
func (b *BoltGraphDB) LoadNodesBatch(ctx context.Context, nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}
	batch := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		props := cloneProps(n.Properties)
		props["id"] = n.ID
		batch = append(batch, map[string]any{
			"id":    n.ID,
			"props": props,
		})
	}

	// UNWIND keeps one planned query; MERGE upserts on unique id (see above).
	const cypher = `
UNWIND $batch AS row
MERGE (n:Person {id: row.id})
SET n += row.props`

	_, err := b.runWrite(ctx, cypher, map[string]any{"batch": batch})
	if err != nil {
		return fmt.Errorf("%s: LoadNodesBatch: %w", b.name, err)
	}
	return nil
}

// LoadRelationshipsBatch bulk-upserts FOLLOWS edges between Person nodes.
//
// Measures: batched write throughput for edge ingest after vertices exist.
//
// Write strategy: MERGE (not CREATE), same rationale as LoadNodesBatch —
// idempotent retries for fair load-throughput comparison; CREATE would be
// faster but produce duplicate FOLLOWS edges on retry.
//
// Returns written = rows that MATCH'd both endpoints and MERGE'd an edge.
// Use RETURN count(rel) (not RelationshipsCreated) so idempotent MERGE retries
// still count as written; only missing endpoints reduce written below len(rels).
func (b *BoltGraphDB) LoadRelationshipsBatch(ctx context.Context, rels []Relationship) (int, error) {
	if len(rels) == 0 {
		return 0, nil
	}
	batch := make([]map[string]any, 0, len(rels))
	for _, r := range rels {
		batch = append(batch, map[string]any{
			"from":  r.FromID,
			"to":    r.ToID,
			"props": cloneProps(r.Properties),
		})
	}

	// Endpoints are MATCH'd (not MERGE'd): a missing/misspelled id does not
	// create a bare Person without name (which would inflate Aggregation and
	// corrupt IndexedLookup). Only the FOLLOWS edge is MERGE'd for idempotency.
	// Note: under UNWIND, a failed MATCH drops that row (no edge written); it
	// does not raise a Cypher error by itself — hence the count(rel) check.
	const cypher = `
UNWIND $batch AS row
MATCH (a:Person {id: row.from})
MATCH (b:Person {id: row.to})
MERGE (a)-[rel:FOLLOWS]->(b)
SET rel += row.props
RETURN count(rel) AS created`

	val, err := b.runWriteSingle(ctx, cypher, map[string]any{"batch": batch})
	if err != nil {
		return 0, fmt.Errorf("%s: LoadRelationshipsBatch: %w", b.name, err)
	}
	written := toInt(val)
	if skipped := len(batch) - written; skipped > 0 {
		log.Printf("warning: %s: LoadRelationshipsBatch skipped %d of %d edges (delta=%d; missing MATCH endpoints)",
			b.name, skipped, len(batch), skipped)
	}
	return written, nil
}

// Traversal expands from startID up to `hops` relationship hops (undirected).
//
// Semantics: (a) cumulative — count of DISTINCT nodes reachable within 1..N
// hops (NOT exact-depth-N only). count(DISTINCT m) dedupes nodes reachable
// via multiple paths. Excludes the start node.
//
// Measures: multi-hop neighborhood expansion / variable-length path cost.
func (b *BoltGraphDB) Traversal(ctx context.Context, startID string, hops int) (int, error) {
	// hops is string-interpolated into [*1..N] (Cypher cannot bind path
	// bounds). Clamp to a tiny allowlist before interpolation.
	if hops < 1 || hops > 3 {
		return 0, fmt.Errorf("%s: Traversal: hops must be in 1..3, got %d", b.name, hops)
	}

	cypher := fmt.Sprintf(`
MATCH (s:Person {id: $startID})-[*1..%d]-(m)
WHERE m <> s
RETURN count(DISTINCT m) AS cnt`, hops)

	val, err := b.runReadSingle(ctx, cypher, map[string]any{"startID": startID})
	if err != nil {
		return 0, fmt.Errorf("%s: Traversal: %w", b.name, err)
	}
	return toInt(val), nil
}

// PointLookup fetches a single Person by primary key id.
//
// Measures: primary-key / unique-constraint point get latency.
func (b *BoltGraphDB) PointLookup(ctx context.Context, id string) (bool, error) {
	const cypher = `
MATCH (n:Person {id: $id})
RETURN n.id AS id
LIMIT 1`

	session := b.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, map[string]any{"id": id})
	if err != nil {
		return false, fmt.Errorf("%s: PointLookup: %w", b.name, err)
	}
	found := result.Next(ctx)
	if err := result.Err(); err != nil {
		return false, fmt.Errorf("%s: PointLookup: %w", b.name, err)
	}
	// Consume surfaces summary/server errors that Run may leave nil.
	if _, err := result.Consume(ctx); err != nil {
		return false, fmt.Errorf("%s: PointLookup: %w", b.name, err)
	}
	return found, nil
}

// IndexedLookup counts Person nodes matching property == value.
// For this benchmark the indexed property is "name".
//
// Measures: secondary-index equality lookup selectivity/latency.
func (b *BoltGraphDB) IndexedLookup(ctx context.Context, property, value string) (int, error) {
	// Dynamic property key via parameter (n[$property]), never concatenated.
	const cypher = `
MATCH (n:Person)
WHERE n[$property] = $value
RETURN count(n) AS cnt`

	val, err := b.runReadSingle(ctx, cypher, map[string]any{
		"property": property,
		"value":    value,
	})
	if err != nil {
		return 0, fmt.Errorf("%s: IndexedLookup: %w", b.name, err)
	}
	return toInt(val), nil
}

// Aggregation returns the total number of Person nodes.
//
// Measures: full-scan or metadata count aggregation over the vertex set.
func (b *BoltGraphDB) Aggregation(ctx context.Context) (int64, error) {
	const cypher = `
MATCH (n:Person)
RETURN count(n) AS cnt`

	val, err := b.runReadSingle(ctx, cypher, map[string]any{})
	if err != nil {
		return 0, fmt.Errorf("%s: Aggregation: %w", b.name, err)
	}
	return toInt64(val), nil
}

// WriteOne upserts a single Person node.
//
// Measures: single-row write / MERGE latency (OLTP-style).
func (b *BoltGraphDB) WriteOne(ctx context.Context, n Node) error {
	props := cloneProps(n.Properties)
	props["id"] = n.ID

	const cypher = `
MERGE (p:Person {id: $id})
SET p += $props`

	_, err := b.runWrite(ctx, cypher, map[string]any{
		"id":    n.ID,
		"props": props,
	})
	if err != nil {
		return fmt.Errorf("%s: WriteOne: %w", b.name, err)
	}
	return nil
}

func (b *BoltGraphDB) runWrite(ctx context.Context, cypher string, params map[string]any) (neo4j.ResultWithContext, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("%s: not connected", b.name)
	}
	session := b.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	if _, err := result.Consume(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// runWriteSingle runs a write query expected to return a single scalar value
// (e.g. count(rel)), checking both Run and Consume errors.
func (b *BoltGraphDB) runWriteSingle(ctx context.Context, cypher string, params map[string]any) (any, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("%s: not connected", b.name)
	}
	session := b.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	if !result.Next(ctx) {
		if err := result.Err(); err != nil {
			return nil, err
		}
		if _, err := result.Consume(ctx); err != nil {
			return nil, err
		}
		return int64(0), nil
	}
	val := result.Record().Values[0]
	if err := result.Err(); err != nil {
		return nil, err
	}
	if _, err := result.Consume(ctx); err != nil {
		return nil, err
	}
	return val, nil
}

func (b *BoltGraphDB) runReadSingle(ctx context.Context, cypher string, params map[string]any) (any, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("%s: not connected", b.name)
	}
	session := b.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	if !result.Next(ctx) {
		if err := result.Err(); err != nil {
			return nil, err
		}
		if _, err := result.Consume(ctx); err != nil {
			return nil, err
		}
		return int64(0), nil
	}
	val := result.Record().Values[0]
	if err := result.Err(); err != nil {
		return nil, err
	}
	// Consume surfaces summary/server errors that Run/Next may leave nil.
	if _, err := result.Consume(ctx); err != nil {
		return nil, err
	}
	return val, nil
}

func cloneProps(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case int32:
		return int(n)
	default:
		return 0
	}
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	default:
		return 0
	}
}
