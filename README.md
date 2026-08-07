# graph-benchmark

Cross-database graph benchmarking tool for CognoDB, Neo4j, Memgraph, FalkorDB, and ArangoDB.

## Requirements

- Go 1.22+
- Running instances of the databases you want to benchmark
- Credentials and URIs provided via environment variables (never hardcode secrets)

## Quick start

```bash
cp .env.example .env
# edit .env with your database URIs and passwords

# Sample dataset, then load one database (or use scripts/load_all.sh)
go run ./cmd/sample -max-edges 300000
go run ./cmd/load -db cognodb

# Run a benchmark category
go run ./cmd/bench_traversal
go run ./cmd/bench_lookup
go run ./cmd/bench_agg_mixed
```

## Layout

| Path | Purpose |
|------|---------|
| `cmd/` | CLI entrypoints (load, sample, smoketest, bench_*) |
| `internal/db/` | Database interface and per-backend implementations |
| `internal/bench/` | Benchmark runner, timer, Workload interface, result schema |
| `internal/loader/` | Dataset download, BFS sample, and import |
| `workloads/` | Benchmark query definitions (traversal, lookup, aggregation) |
| `datasets/` | Local dataset files (CSVs are gitignored) |
| `results/` | Load + bench JSON outputs (gitignored) |
| `scripts/` | Helper scripts (download, load_all) |
| `demo/` | Framework self-checks only (not Phase 5 results) |

## Configuration

Copy `.env.example` to `.env` and set:

- `COGNODB_URI`, `COGNODB_PASSWORD`
- `NEO4J_URI`, `NEO4J_PASSWORD`
- `MEMGRAPH_URI`, `MEMGRAPH_PASSWORD`
- `FALKORDB_URI`, `FALKORDB_PASSWORD`
- `ARANGODB_URI`, `ARANGODB_USER`, `ARANGODB_PASSWORD`

## Dependencies

- [neo4j-go-driver/v5](https://github.com/neo4j/neo4j-go-driver) - Neo4j / Bolt clients
- [godotenv](https://github.com/joho/godotenv) - load `.env`
- [arangodb/go-driver](https://github.com/arangodb/go-driver) - ArangoDB AQL client
