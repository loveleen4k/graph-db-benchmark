# Demo / self-check tools

These are **not** Phase 5 benchmark measurements. Do not mix their outputs into
final result tables.

| Path | Purpose |
|------|---------|
| `benchdemo/` | Framework self-check (ping + point lookup). Writes under `demo/results/`. |
| `smoke_skipped_edges/` | Confirms skipped-edge counting during relationship load. |

Run examples:

```bash
go run ./demo/benchdemo [database]
go run ./demo/smoke_skipped_edges
```
