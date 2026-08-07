// Aggregation (Phase 5 Part 3):
//
// Count all Person nodes (label/type = Person). Same semantics on every
// database via GraphDB.Aggregation — Cypher MATCH (n:Person) RETURN count(n),
// AQL LENGTH(Person) / equivalent.
package workloads

import (
	"context"
	"fmt"

	"graph-benchmark/internal/bench"
	"graph-benchmark/internal/db"
)

// AggregationWorkload counts all Person nodes.
type AggregationWorkload struct{}

func NewAggregationWorkload() *AggregationWorkload { return &AggregationWorkload{} }

func (a *AggregationWorkload) Name() string         { return "aggregation" }
func (a *AggregationWorkload) Category() string     { return "aggregation" }
func (a *AggregationWorkload) NeedsStartNode() bool { return false }

func (a *AggregationWorkload) Execute(ctx context.Context, gdb db.GraphDB, _ string) error {
	n, err := gdb.Aggregation(ctx)
	if err != nil {
		return err
	}
	if n < 1 {
		return fmt.Errorf("aggregation: expected Person count >= 1, got %d", n)
	}
	return nil
}

var _ bench.Workload = (*AggregationWorkload)(nil)
