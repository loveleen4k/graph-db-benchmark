package db

import "context"

// Node is a graph vertex used for load and write workloads.
// Benchmark schema: label Person with unique property "id" and indexed "name".
type Node struct {
	ID         string
	Labels     []string
	Properties map[string]any
}

// Relationship is a directed edge used for load workloads.
// Benchmark schema: type FOLLOWS between Person nodes.
type Relationship struct {
	ID         string
	FromID     string
	ToID       string
	Type       string
	Properties map[string]any
}

// GraphDB is the common interface every backend must implement so workloads
// and metrics stay database-agnostic. Implementations must use parameterized
// queries only (no string-concatenated user values in query text).
type GraphDB interface {
	Name() string
	Connect(ctx context.Context) error
	Close(ctx context.Context) error
	Ping(ctx context.Context) error
	CreateSchema(ctx context.Context) error
	LoadNodesBatch(ctx context.Context, nodes []Node) error
	// LoadRelationshipsBatch writes edges and returns how many rows survived
	// endpoint resolution (MATCH/FILTER). Callers compute skipped as
	// len(rels)-written; silent MATCH misses must not inflate node counts.
	LoadRelationshipsBatch(ctx context.Context, rels []Relationship) (written int, err error)
	Traversal(ctx context.Context, startID string, hops int) (int, error) // directed 1..N hop distinct count
	PointLookup(ctx context.Context, id string) (bool, error)
	IndexedLookup(ctx context.Context, property, value string) (int, error)
	Aggregation(ctx context.Context) (int64, error)
	WriteOne(ctx context.Context, n Node) error
}
