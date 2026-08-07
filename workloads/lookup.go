// Package workloads - lookup definitions (Phase 5 Part 2).
//
// Point lookup: fetch a single node by its unique ID (primary key lookup).
//
// Indexed/filtered lookup: fetch node(s) matching a non-ID property (here:
// Person.name), where that property is indexed on every platform.
//
// Filtered property for this benchmark: "name" (values like user_<id> from
// nodes.csv). Indexes are created by GraphDB.CreateSchema on all five backends
// (Neo4j/CognoDB: CREATE INDEX ON Person.name; Memgraph: CREATE INDEX ON
// :Person(name); FalkorDB: CREATE INDEX FOR (p:Person) ON (p.name); ArangoDB:
// persistent index idx_person_name on Person.name). Point lookup uses Person.id
// / document _key.
package workloads

import (
	"context"
	"fmt"

	"graph-benchmark/internal/bench"
	"graph-benchmark/internal/db"
)

const FilteredLookupProperty = "name"

// PointLookupWorkload fetches one Person by unique id (seed node rotation).
type PointLookupWorkload struct{}

func NewPointLookupWorkload() *PointLookupWorkload { return &PointLookupWorkload{} }

func (p *PointLookupWorkload) Name() string         { return "lookup_point" }
func (p *PointLookupWorkload) Category() string     { return "lookup_point" }
func (p *PointLookupWorkload) NeedsStartNode() bool { return true }

func (p *PointLookupWorkload) Execute(ctx context.Context, gdb db.GraphDB, startNode string) error {
	if startNode == "" {
		return fmt.Errorf("lookup_point: empty start node")
	}
	found, err := gdb.PointLookup(ctx, startNode)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("lookup_point: id %q not found", startNode)
	}
	return nil
}

// FilteredLookupWorkload looks up Person nodes by indexed property "name".
// For seed id S the expected name is user_S (written by the sampler).
type FilteredLookupWorkload struct{}

func NewFilteredLookupWorkload() *FilteredLookupWorkload { return &FilteredLookupWorkload{} }

func (f *FilteredLookupWorkload) Name() string         { return "lookup_filtered" }
func (f *FilteredLookupWorkload) Category() string     { return "lookup_filtered" }
func (f *FilteredLookupWorkload) NeedsStartNode() bool { return true }

func (f *FilteredLookupWorkload) Execute(ctx context.Context, gdb db.GraphDB, startNode string) error {
	if startNode == "" {
		return fmt.Errorf("lookup_filtered: empty start node")
	}
	name := "user_" + startNode
	n, err := gdb.IndexedLookup(ctx, FilteredLookupProperty, name)
	if err != nil {
		return err
	}
	if n < 1 {
		return fmt.Errorf("lookup_filtered: name %q matched %d nodes", name, n)
	}
	return nil
}

var (
	_ bench.Workload = (*PointLookupWorkload)(nil)
	_ bench.Workload = (*FilteredLookupWorkload)(nil)
)
