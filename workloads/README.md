# Workload query definitions (Phase 5)

This directory holds the real benchmark query implementations per category.

## Traversal (`traversal.go`)

Hop definition (identical on every database):

> N-hop traversal = count of distinct nodes reachable within 1 to N hops
> outward from the start node, excluding the start node itself, following
> relationships in their stored direction.

Start nodes always come from `datasets/sample_seed_nodes.csv` via the Phase 4
runner rotation.

## Other categories (later)

- lookup/
- aggregation/
- mixed/

Framework-only example workloads (`framework_check_ping`,
`framework_check_point_lookup`) live in `internal/bench` for demos and tests.
Their result files use category `framework_check` and must not be mixed into
Phase 5 result tables.
