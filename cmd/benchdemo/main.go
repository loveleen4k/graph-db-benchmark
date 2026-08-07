// Command benchdemo is a FRAMEWORK SELF-CHECK only - not a Phase 5 benchmark.
//
// It exercises the runner against one live database (default: cognodb) with
// trivial example workloads. Result files are written as
// results/bench_framework_check_<db>_*.json so they must not be pulled into
// final Phase 5 result tables (those use categories like traversal, lookup,
// aggregation, mixed).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"graph-benchmark/internal/bench"
	"graph-benchmark/internal/db"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	fmt.Println("============================================================")
	fmt.Println("FRAMEWORK SELF-CHECK (cmd/benchdemo) - NOT a Phase 5 result")
	fmt.Println("Outputs: results/bench_framework_check_<db>_*.json")
	fmt.Println("Do not include these in final benchmark tables.")
	fmt.Println("============================================================")

	dbName := "cognodb"
	if len(os.Args) > 1 {
		dbName = os.Args[1]
	}

	dbs, err := db.LoadFromEnv()
	if err != nil {
		fatal(err)
	}
	g, ok := dbs[dbName]
	if !ok {
		fatal(fmt.Errorf("database %q not configured", dbName))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := g.Connect(ctx); err != nil {
		fatal(err)
	}
	defer g.Close(ctx)

	seeds, err := bench.LoadStartNodes(bench.DefaultSeedNodesPath)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("loaded %d start nodes from %s\n", len(seeds), bench.DefaultSeedNodesPath)

	runner := bench.NewRunner(g)

	// 1) Ping workload - no start node
	ping := bench.ExamplePingWorkload()
	pingRes, err := runner.Run(ctx, ping, bench.Config{
		Iterations:       20,
		WarmupIterations: 5,
		StartNodes:       seeds,
	})
	if err != nil {
		fatal(err)
	}
	pingPath, err := bench.WriteResult(*pingRes)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("[framework_check] ping result -> %s\n", pingPath)
	printResult(pingRes)

	// 2) Point lookup over shared seeds
	lookup := bench.ExamplePointLookupWorkload()
	lookRes, err := runner.Run(ctx, lookup, bench.Config{
		Iterations:       20,
		WarmupIterations: 5,
		StartNodes:       seeds,
	})
	if err != nil {
		fatal(err)
	}
	lookPath, err := bench.WriteResult(*lookRes)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("[framework_check] lookup result -> %s\n", lookPath)
	printResult(lookRes)

	// Schema checks
	for _, path := range []string{pingPath, lookPath} {
		if err := validateSchema(path); err != nil {
			fatal(fmt.Errorf("%s: %w", path, err))
		}
		fmt.Printf("schema OK: %s\n", path)
	}

	if pingRes.Category != "framework_check" || lookRes.Category != "framework_check" {
		fatal(fmt.Errorf("expected category framework_check, got ping=%q lookup=%q",
			pingRes.Category, lookRes.Category))
	}
	if pingRes.WarmupIterations != 5 || lookRes.WarmupIterations != 5 {
		fatal(fmt.Errorf("warmup_iterations not recorded correctly"))
	}
	if pingRes.Iterations != 20 || lookRes.Iterations != 20 {
		fatal(fmt.Errorf("iterations not recorded correctly"))
	}
	if lookRes.P50Ms <= 0 || lookRes.P95Ms < lookRes.P50Ms {
		fatal(fmt.Errorf("lookup percentiles look wrong: p50=%v p95=%v", lookRes.P50Ms, lookRes.P95Ms))
	}
	if lookRes.Failures != 0 || pingRes.Failures != 0 {
		fatal(fmt.Errorf("unexpected failures: ping=%d lookup=%d", pingRes.Failures, lookRes.Failures))
	}

	fmt.Println("============================================================")
	fmt.Println("benchdemo FRAMEWORK SELF-CHECK: PASS (not a Phase 5 measurement)")
	fmt.Println("============================================================")
}

func printResult(r *bench.Result) {
	fmt.Printf("  [framework_check] database=%s workload=%s category=%s iters=%d warmup=%d p50_ms=%.3f p95_ms=%.3f failures=%d\n",
		r.Database, r.WorkloadName, r.Category, r.Iterations, r.WarmupIterations, r.P50Ms, r.P95Ms, r.Failures)
}

func validateSchema(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	required := []string{
		"database", "workload_name", "iterations", "warmup_iterations",
		"p50_ms", "p95_ms", "failures", "timestamp",
	}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			return fmt.Errorf("missing field %q", k)
		}
	}
	if cat, _ := m["category"].(string); cat != "framework_check" {
		return fmt.Errorf("category=%q want framework_check", cat)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "benchdemo FAIL:", err)
	os.Exit(1)
}
