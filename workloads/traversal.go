// Package workloads holds Phase 5 benchmark query definitions.
//
// Hop definition (used by every database's traversal query):
//
//	N-hop traversal = count of distinct nodes reachable within 1 to N hops
//	outward from the start node, excluding the start node itself, following
//	relationships in their stored direction.
//
// In this benchmark the stored relationship type is FOLLOWS (from_id -> to_id).
// Cypher:  (s)-[:FOLLOWS*1..N]->(m) with count(DISTINCT m), m <> s
// AQL:     FOR v IN 1..N OUTBOUND start GRAPH social ... COLLECT WITH COUNT
// FalkorDB uses the same Cypher text as the Bolt backends over RESP.
package workloads

import (
	"context"
	"fmt"

	"graph-benchmark/internal/bench"
	"graph-benchmark/internal/db"
)

// TraversalHop is a Phase 5 traversal workload for a fixed hop depth (1, 2, or 3).
// Start nodes come from the Phase 4 runner's sample_seed_nodes.csv rotation.
type TraversalHop struct {
	Hops int
}

// NewTraversalHop returns a workload for hop depth 1, 2, or 3.
func NewTraversalHop(hops int) (*TraversalHop, error) {
	if hops < 1 || hops > 3 {
		return nil, fmt.Errorf("traversal hops must be 1..3, got %d", hops)
	}
	return &TraversalHop{Hops: hops}, nil
}

func (t *TraversalHop) Name() string {
	return fmt.Sprintf("traversal_%dhop", t.Hops)
}

// Category becomes the filename segment: bench_traversal_1hop_<db>_....json
func (t *TraversalHop) Category() string {
	return fmt.Sprintf("traversal_%dhop", t.Hops)
}

func (t *TraversalHop) NeedsStartNode() bool { return true }

func (t *TraversalHop) Execute(ctx context.Context, gdb db.GraphDB, startNode string) error {
	if startNode == "" {
		return fmt.Errorf("%s: empty start node", t.Name())
	}
	_, err := gdb.Traversal(ctx, startNode, t.Hops)
	return err
}

// Ensure TraversalHop implements bench.Workload.
var _ bench.Workload = (*TraversalHop)(nil)
