// Command bench_traversal runs Phase 5 Part 1: 1/2/3-hop traversal benchmarks
// on all five databases using the Phase 4 runner (warm-up, p50/p95, failures).
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"graph-benchmark/internal/bench"
	"graph-benchmark/internal/db"
	"graph-benchmark/workloads"

	"github.com/joho/godotenv"
)

var (
	dbOrder  = []string{"arangodb", "cognodb", "falkordb", "memgraph", "neo4j"}
	hopDepths = []int{1, 2, 3}
)

const measuredIterations = 100

func main() {
	_ = godotenv.Load()

	seeds, err := bench.LoadStartNodes(bench.DefaultSeedNodesPath)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("start nodes: %d (from %s)\n", len(seeds), bench.DefaultSeedNodesPath)

	dbs, err := db.LoadFromEnv()
	if err != nil {
		fatal(err)
	}

	// Generous timeout: 3 hops x 5 DBs x (10 warm + 100 measured).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()

	type row struct {
		DB, Hop, Path string
		P50, P95      float64
		Failures      int
	}
	var summary []row

	for _, name := range dbOrder {
		g, ok := dbs[name]
		if !ok {
			fatal(fmt.Errorf("missing database %q", name))
		}
		fmt.Printf("\n========== %s ==========\n", name)
		if err := g.Connect(ctx); err != nil {
			fatal(fmt.Errorf("%s connect: %w", name, err))
		}

		runner := bench.NewRunner(g)
		for _, hops := range hopDepths {
			w, err := workloads.NewTraversalHop(hops)
			if err != nil {
				_ = g.Close(ctx)
				fatal(err)
			}
			fmt.Printf("--- %s ---\n", w.Name())
			res, err := runner.Run(ctx, w, bench.Config{
				Iterations:       measuredIterations,
				WarmupIterations: bench.DefaultWarmupIterations,
				StartNodes:       seeds,
			})
			if err != nil {
				_ = g.Close(ctx)
				fatal(fmt.Errorf("%s %s: %w", name, w.Name(), err))
			}
			path, err := bench.WriteResult(*res)
			if err != nil {
				_ = g.Close(ctx)
				fatal(err)
			}
			fmt.Printf("  wrote %s\n", path)
			fmt.Printf("  p50_ms=%.3f p95_ms=%.3f failures=%d warmup=%d iters=%d\n",
				res.P50Ms, res.P95Ms, res.Failures, res.WarmupIterations, res.Iterations)
			if res.P50Ms > 0 && res.P95Ms/res.P50Ms >= 10 {
				fmt.Printf("  FLAG: p95/p50 = %.1fx (wide spread - some seeds may have large neighborhoods)\n",
					res.P95Ms/res.P50Ms)
			}
			summary = append(summary, row{
				DB: name, Hop: w.Name(), Path: path,
				P50: res.P50Ms, P95: res.P95Ms, Failures: res.Failures,
			})
		}
		if err := g.Close(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s close: %v\n", name, err)
		}
	}

	fmt.Println("\n=== SUMMARY ===")
	fmt.Printf("%-12s %-18s %10s %10s %8s\n", "database", "workload", "p50_ms", "p95_ms", "fail")
	for _, r := range summary {
		fmt.Printf("%-12s %-18s %10.3f %10.3f %8d\n", r.DB, r.Hop, r.P50, r.P95, r.Failures)
	}
	fmt.Printf("\n%d result files written\n", len(summary))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bench_traversal FAIL:", err)
	os.Exit(1)
}
