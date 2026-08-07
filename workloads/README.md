# Workload query definitions (Phase 5)

This directory holds the real benchmark query implementations per category:

- traversal/
- lookup/
- aggregation/
- mixed/

The runner and interfaces live in `internal/bench`. Phase 5 plugs category
workloads into `bench.Workload` without changing the runner.

Framework-only example workloads (`framework_check_ping`,
`framework_check_point_lookup`) live in `internal/bench` for demos and tests.
Their result files use category `framework_check` and must not be mixed into
Phase 5 result tables.
