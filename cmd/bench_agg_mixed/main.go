// Command bench_agg_mixed runs Phase 5 Parts 3–4: aggregation + mixed workload
// on all five databases.
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

var dbOrder = []string{"arangodb", "cognodb", "falkordb", "memgraph", "neo4j"}

func main() {
	_ = godotenv.Load()

	seeds, err := bench.LoadStartNodes(bench.DefaultSeedNodesPath)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("seeds: %d\n", len(seeds))
	fmt.Println("aggregation: count all Person nodes")
	fmt.Println("mixed: 10 clients, 500 requests, 80% point lookup / 20% WriteOne")

	dbs, err := db.LoadFromEnv()
	if err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	fmt.Println("\n=== AGGREGATION ===")
	type aggRow struct {
		DB           string
		P50, P95     float64
		Fail         int
		Path         string
	}
	var aggs []aggRow

	for _, name := range dbOrder {
		g, ok := dbs[name]
		if !ok {
			fatal(fmt.Errorf("missing %s", name))
		}
		fmt.Printf("\n--- %s ---\n", name)
		if err := g.Connect(ctx); err != nil {
			fatal(fmt.Errorf("%s connect: %w", name, err))
		}
		runner := bench.NewRunner(g)
		w := workloads.NewAggregationWorkload()
		res, err := runner.Run(ctx, w, bench.Config{
			Iterations:       100,
			WarmupIterations: bench.DefaultWarmupIterations,
		})
		if err != nil {
			_ = g.Close(ctx)
			fatal(fmt.Errorf("%s aggregation: %w", name, err))
		}
		path, err := bench.WriteResult(*res)
		if err != nil {
			_ = g.Close(ctx)
			fatal(err)
		}
		fmt.Printf("  p50_ms=%.3f p95_ms=%.3f failures=%d -> %s\n",
			res.P50Ms, res.P95Ms, res.Failures, path)
		if res.P50Ms > 0 && res.P95Ms/res.P50Ms >= 10 {
			fmt.Printf("  FLAG: p95/p50=%.1fx\n", res.P95Ms/res.P50Ms)
		}
		aggs = append(aggs, aggRow{DB: name, P50: res.P50Ms, P95: res.P95Ms, Fail: res.Failures, Path: path})
		_ = g.Close(ctx)
	}

	fmt.Println("\n=== MIXED (10 clients, 500 req, 80/20 read/write) ===")
	type mixRow struct {
		DB           string
		P50, P95, QPS float64
		Fail         int
		Path         string
	}
	var mixes []mixRow

	for _, name := range dbOrder {
		g := dbs[name]
		fmt.Printf("\n--- %s ---\n", name)
		if err := g.Connect(ctx); err != nil {
			fatal(fmt.Errorf("%s connect: %w", name, err))
		}
		runner := bench.NewRunner(g)
		res, err := runner.RunMixed(ctx, bench.MixedConfig{
			Concurrency:    bench.DefaultMixedConcurrency,
			TotalRequests:  bench.DefaultMixedTotalRequests,
			ReadPct:        bench.DefaultMixedReadPct,
			WarmupRequests: bench.DefaultWarmupIterations,
			StartNodes:     seeds,
		})
		if err != nil {
			_ = g.Close(ctx)
			fatal(fmt.Errorf("%s mixed: %w", name, err))
		}
		path, err := bench.WriteResult(*res)
		if err != nil {
			_ = g.Close(ctx)
			fatal(err)
		}
		fmt.Printf("  qps=%.1f p50_ms=%.3f p95_ms=%.3f failures=%d duration=%.2fs -> %s\n",
			res.QueriesPerSec, res.P50Ms, res.P95Ms, res.Failures, res.DurationSec, path)
		if res.P50Ms > 0 && res.P95Ms/res.P50Ms >= 10 {
			fmt.Printf("  FLAG: p95/p50=%.1fx\n", res.P95Ms/res.P50Ms)
		}
		mixes = append(mixes, mixRow{
			DB: name, P50: res.P50Ms, P95: res.P95Ms, QPS: res.QueriesPerSec,
			Fail: res.Failures, Path: path,
		})
		_ = g.Close(ctx)
	}

	fmt.Println("\n=== AGGREGATION SUMMARY ===")
	fmt.Printf("%-12s %10s %10s %8s\n", "database", "p50_ms", "p95_ms", "fail")
	for _, r := range aggs {
		fmt.Printf("%-12s %10.3f %10.3f %8d\n", r.DB, r.P50, r.P95, r.Fail)
	}

	fmt.Println("\n=== MIXED SUMMARY ===")
	fmt.Printf("%-12s %10s %10s %10s %8s\n", "database", "qps", "p50_ms", "p95_ms", "fail")
	for _, r := range mixes {
		fmt.Printf("%-12s %10.1f %10.3f %10.3f %8d\n", r.DB, r.QPS, r.P50, r.P95, r.Fail)
	}
	fmt.Printf("\n%d aggregation + %d mixed result files\n", len(aggs), len(mixes))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bench_agg_mixed FAIL:", err)
	os.Exit(1)
}
