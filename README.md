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

go run ./cmd
```

## Layout

| Path | Purpose |
|------|---------|
| `cmd/` | CLI entrypoint |
| `internal/db/` | Database interface and per-backend implementations |
| `internal/workload/` | Benchmark workload definitions |
| `internal/metrics/` | Timing, percentiles, result aggregation |
| `internal/loader/` | Dataset download, clean, and import |
| `datasets/` | Local dataset files (CSVs are gitignored) |
| `results/` | Benchmark JSON outputs (gitignored) |
| `scripts/` | Helper scripts |

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
