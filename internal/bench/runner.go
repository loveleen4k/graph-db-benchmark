package bench

import (
	"context"
	"fmt"
	"log"
	"time"

	"graph-benchmark/internal/db"
)

const (
	DefaultWarmupIterations = 10
	DefaultIterations       = 50
)

// Config controls a Runner.Run invocation.
type Config struct {
	// Iterations is the number of measured runs (after warm-up). Default 50.
	Iterations int
	// WarmupIterations are executed first and discarded from percentiles.
	// When 0 and DisableWarmup is false, DefaultWarmupIterations (10) is used.
	WarmupIterations int
	// DisableWarmup forces zero warm-up even when WarmupIterations is 0.
	DisableWarmup bool
	// StartNodes is the shared seed list (typically from sample_seed_nodes.csv).
	// Required when the workload NeedsStartNode(); rotated round-robin so every
	// database sees the same start ids in the same order.
	StartNodes []string
}

// Runner executes workloads against one GraphDB with consistent timing.
type Runner struct {
	DB db.GraphDB
}

// NewRunner wraps a GraphDB. Connect before Run if the driver requires it.
func NewRunner(gdb db.GraphDB) *Runner {
	return &Runner{DB: gdb}
}

// DefaultConfig returns Iterations=50, Warmup=10, and the given start nodes.
func DefaultConfig(startNodes []string) Config {
	return Config{
		Iterations:       DefaultIterations,
		WarmupIterations: DefaultWarmupIterations,
		StartNodes:       startNodes,
	}
}

func (cfg Config) resolvedWarmup() int {
	if cfg.DisableWarmup {
		return 0
	}
	if cfg.WarmupIterations <= 0 {
		return DefaultWarmupIterations
	}
	return cfg.WarmupIterations
}

func (cfg Config) resolvedIterations() int {
	if cfg.Iterations <= 0 {
		return DefaultIterations
	}
	return cfg.Iterations
}

// Run warms up, then measures iterations.
//
// Warm-up iterations are never included in p50/p95.
// Measured failures are counted in Result.Failures; only successful iteration
// latencies enter the percentile math (so error paths do not inflate p50/p95).
func (r *Runner) Run(ctx context.Context, w Workload, cfg Config) (*Result, error) {
	if r == nil || r.DB == nil {
		return nil, fmt.Errorf("runner: nil database")
	}
	if w == nil {
		return nil, fmt.Errorf("runner: nil workload")
	}

	iterations := cfg.resolvedIterations()
	warmup := cfg.resolvedWarmup()

	if w.NeedsStartNode() && len(cfg.StartNodes) == 0 {
		return nil, fmt.Errorf("workload %q requires StartNodes (load %s)", w.Name(), DefaultSeedNodesPath)
	}

	seedAt := 0
	nextSeed := func() string {
		if !w.NeedsStartNode() || len(cfg.StartNodes) == 0 {
			return ""
		}
		id := cfg.StartNodes[seedAt%len(cfg.StartNodes)]
		seedAt++
		return id
	}

	// Warm-up: executed and timed, then discarded.
	for i := 0; i < warmup; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var t Timer
		t.Start()
		err := w.Execute(ctx, r.DB, nextSeed())
		t.Stop()
		if err != nil {
			log.Printf("bench warm-up iter %d failed (excluded from stats): %v", i+1, err)
		}
		_ = t.Elapsed()
	}

	latencies := make([]time.Duration, 0, iterations)
	failures := 0
	var failSamples []string

	for i := 0; i < iterations; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var t Timer
		t.Start()
		err := w.Execute(ctx, r.DB, nextSeed())
		t.Stop()
		elapsed := t.Elapsed()

		if err != nil {
			failures++
			if len(failSamples) < 5 {
				failSamples = append(failSamples, err.Error())
			}
			continue
		}
		latencies = append(latencies, elapsed)
	}

	for _, s := range failSamples {
		log.Printf("bench failure sample (%s/%s): %s", r.DB.Name(), w.Name(), s)
	}
	if failures > 0 {
		log.Printf("bench %s/%s: %d/%d measured iterations failed",
			r.DB.Name(), w.Name(), failures, iterations)
	}

	return &Result{
		Database:         r.DB.Name(),
		WorkloadName:     w.Name(),
		Category:         w.Category(),
		Iterations:       iterations,
		WarmupIterations: warmup,
		P50Ms:            durationToMs(Percentile(latencies, 50)),
		P95Ms:            durationToMs(Percentile(latencies, 95)),
		Failures:         failures,
		Timestamp:        time.Now().UTC(),
	}, nil
}
