package db

// Memgraph wraps BoltGraphDB for Memgraph's Bolt/Cypher interface.
// Quirk: Memgraph's constraint/index DDL differs from Neo4j 5 — CreateSchema
// may need Memgraph-specific statements (CREATE INDEX ON :Label(prop)).
type Memgraph struct {
	*BoltGraphDB
}

// NewMemgraph returns a Memgraph client configured from environment values.
func NewMemgraph(uri, username, password string) *Memgraph {
	return &Memgraph{BoltGraphDB: NewBoltGraphDB("memgraph", uri, username, password)}
}
