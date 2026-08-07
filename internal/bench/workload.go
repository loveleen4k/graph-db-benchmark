package bench

import (
	"context"

	"graph-benchmark/internal/db"
)

// Workload is one benchmark operation Phase 5 will implement (traversal,
// lookup, aggregation, mixed). The runner times Execute; workloads must not
// start their own clocks.
//
// startNode is a Person.id from the shared sample_seed_nodes.csv list when the
// workload needs a start point. It may be empty for workloads that do not
// (e.g. full-graph aggregation).
type Workload interface {
	Name() string
	// Category is a short label used in result filenames
	// (e.g. "traversal", "lookup", "aggregation", "mixed", "example").
	Category() string
	// NeedsStartNode reports whether Execute should receive a rotating seed id.
	NeedsStartNode() bool
	// Execute runs one query iteration against db. Return a non-nil error to
	// mark the iteration as a failure (still timed; counted in Failures).
	Execute(ctx context.Context, gdb db.GraphDB, startNode string) error
}

// WorkloadFunc adapts a function to Workload.
type WorkloadFunc struct {
	OpName     string
	OpCategory string
	UseSeed    bool
	Fn         func(ctx context.Context, gdb db.GraphDB, startNode string) error
}

func (w WorkloadFunc) Name() string     { return w.OpName }
func (w WorkloadFunc) Category() string { return w.OpCategory }
func (w WorkloadFunc) NeedsStartNode() bool {
	return w.UseSeed
}
func (w WorkloadFunc) Execute(ctx context.Context, gdb db.GraphDB, startNode string) error {
	return w.Fn(ctx, gdb, startNode)
}

// ExamplePingWorkload returns a trivial Ping-based workload for framework demos.
// Category is "framework_check" so outputs are never mistaken for Phase 5 results.
func ExamplePingWorkload() Workload {
	return WorkloadFunc{
		OpName:     "framework_check_ping",
		OpCategory: "framework_check",
		UseSeed:    false,
		Fn: func(ctx context.Context, gdb db.GraphDB, _ string) error {
			return gdb.Ping(ctx)
		},
	}
}

// ExamplePointLookupWorkload returns a trivial point-lookup workload that uses
// the shared start-node list (same IDs on every database).
// Category is "framework_check" so outputs are never mistaken for Phase 5 results.
func ExamplePointLookupWorkload() Workload {
	return WorkloadFunc{
		OpName:     "framework_check_point_lookup",
		OpCategory: "framework_check",
		UseSeed:    true,
		Fn: func(ctx context.Context, gdb db.GraphDB, startNode string) error {
			_, err := gdb.PointLookup(ctx, startNode)
			return err
		},
	}
}
