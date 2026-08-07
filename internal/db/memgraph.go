package db

import (
	"context"
	"fmt"
	"strings"
)

// Memgraph wraps BoltGraphDB for Memgraph's Bolt/Cypher interface.
// Quirk: Memgraph's constraint/index DDL differs from Neo4j 5 - CreateSchema
// uses Memgraph-native CREATE INDEX / CREATE CONSTRAINT forms.
type Memgraph struct {
	*BoltGraphDB
}

// NewMemgraph returns a Memgraph client configured from environment values.
func NewMemgraph(uri, username, password string) *Memgraph {
	return &Memgraph{BoltGraphDB: NewBoltGraphDB("memgraph", uri, username, password)}
}

// CreateSchema installs Person.id uniqueness and a Person.name index using
// Memgraph DDL (not Neo4j 5 IF NOT EXISTS constraint syntax).
func (m *Memgraph) CreateSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE INDEX ON :Person(id)`,
		`CREATE INDEX ON :Person(name)`,
		`CREATE CONSTRAINT ON (p:Person) ASSERT p.id IS UNIQUE`,
	}
	for _, cypher := range stmts {
		if _, err := m.runWrite(ctx, cypher, nil); err != nil && !isMemgraphAlreadyExists(err) {
			return fmt.Errorf("memgraph: CreateSchema: %w", err)
		}
	}
	return nil
}

func isMemgraphAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "already present") ||
		strings.Contains(msg, "index already exists") ||
		strings.Contains(msg, "constraint already exists") ||
		strings.Contains(msg, "exists in the database")
}
