package db

// Neo4j wraps BoltGraphDB for Neo4j Community/Enterprise.
// Quirk: constraint/index DDL syntax here targets Neo4j 5+ (IF NOT EXISTS);
// older 4.x servers need different CREATE CONSTRAINT forms.
type Neo4j struct {
	*BoltGraphDB
}

// NewNeo4j returns a Neo4j client configured from environment values.
func NewNeo4j(uri, username, password string) *Neo4j {
	return &Neo4j{BoltGraphDB: NewBoltGraphDB("neo4j", uri, username, password)}
}
