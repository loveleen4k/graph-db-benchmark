# Workload query definitions

This directory holds the benchmark query implementations per category.

| File | Category |
|------|----------|
| `traversal.go` | 1/2/3-hop outbound distinct-node counts |
| `lookup.go` | Point lookup by id + filtered lookup by `Person.name` |
| `aggregation.go` | Count all Person nodes |
| `csv_check.go` | CSV ground-truth helpers for equivalence checks |

Hop definition (identical on every database):

> N-hop traversal = count of distinct nodes reachable within 1 to N hops
> outward from the start node, excluding the start node itself, following
> relationships in their stored direction.

Start nodes always come from `datasets/sample_seed_nodes.csv` via the shared
runner rotation.

Framework-only example workloads (`framework_check_ping`,
`framework_check_point_lookup`) live in `internal/bench` for unit tests and
`demo/benchdemo`. Their outputs go under `demo/results/` and must not be mixed
into Phase 5 result tables.
