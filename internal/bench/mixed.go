package bench

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"graph-benchmark/internal/db"
)

// MixedConfig controls the concurrent mixed read/write benchmark.
type MixedConfig struct {
	// Concurrency is the number of parallel clients (default 10).
	Concurrency int
	// TotalRequests is the fixed measured request count (default 500).
	TotalRequests int
	// ReadPct is the percent of requests that are point lookups (default 80).
	// The rest are WriteOne creates. Deterministic: write when i%5==0 for 80/20.
	ReadPct int
	// WarmupRequests are sequential ops discarded before the timed phase (default 10).
	WarmupRequests int
	// StartNodes supplies ids for point-lookup reads.
	StartNodes []string
}

const (
	DefaultMixedConcurrency   = 10
	DefaultMixedTotalRequests = 500
	DefaultMixedReadPct       = 80
)

func (c MixedConfig) normalize() MixedConfig {
	if c.Concurrency <= 0 {
		c.Concurrency = DefaultMixedConcurrency
	}
	if c.TotalRequests <= 0 {
		c.TotalRequests = DefaultMixedTotalRequests
	}
	if c.ReadPct <= 0 || c.ReadPct >= 100 {
		c.ReadPct = DefaultMixedReadPct
	}
	if c.WarmupRequests < 0 {
		c.WarmupRequests = DefaultWarmupIterations
	} else if c.WarmupRequests == 0 {
		c.WarmupRequests = DefaultWarmupIterations
	}
	return c
}

// RunMixed runs a concurrent 80/20 point-lookup / WriteOne workload.
// Uses the same Timer + Percentile helpers as sequential Run.
func (r *Runner) RunMixed(ctx context.Context, cfg MixedConfig) (*Result, error) {
	if r == nil || r.DB == nil {
		return nil, fmt.Errorf("runner: nil database")
	}
	cfg = cfg.normalize()
	if len(cfg.StartNodes) == 0 {
		return nil, fmt.Errorf("mixed: StartNodes required for reads")
	}

	// Sequential warm-up (not counted).
	for i := 0; i < cfg.WarmupRequests; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id := cfg.StartNodes[i%len(cfg.StartNodes)]
		if i%5 == 0 {
			_ = r.DB.WriteOne(ctx, mixedNode(r.DB.Name(), i))
		} else {
			_, _ = r.DB.PointLookup(ctx, id)
		}
	}

	type sample struct {
		d   time.Duration
		err error
	}
	jobs := make(chan int, cfg.TotalRequests)
	out := make(chan sample, cfg.TotalRequests)

	var wg sync.WaitGroup
	var writeSeq atomic.Int64

	worker := func() {
		defer wg.Done()
		for i := range jobs {
			if err := ctx.Err(); err != nil {
				out <- sample{err: err}
				continue
			}
			var t Timer
			t.Start()
			var err error
			// Deterministic 80/20: every 5th request is a write.
			if i%5 == 0 {
				n := int(writeSeq.Add(1))
				err = r.DB.WriteOne(ctx, mixedNode(r.DB.Name(), n))
			} else {
				id := cfg.StartNodes[i%len(cfg.StartNodes)]
				var found bool
				found, err = r.DB.PointLookup(ctx, id)
				if err == nil && !found {
					err = fmt.Errorf("point lookup miss for %s", id)
				}
			}
			t.Stop()
			out <- sample{d: t.Elapsed(), err: err}
		}
	}

	for w := 0; w < cfg.Concurrency; w++ {
		wg.Add(1)
		go worker()
	}

	wallStart := time.Now()
	for i := 0; i < cfg.TotalRequests; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(out)
	wall := time.Since(wallStart)

	latencies := make([]time.Duration, 0, cfg.TotalRequests)
	failures := 0
	for s := range out {
		if s.err != nil {
			failures++
			continue
		}
		latencies = append(latencies, s.d)
	}
	if failures > 0 {
		log.Printf("bench mixed %s: %d/%d failed", r.DB.Name(), failures, cfg.TotalRequests)
	}

	secs := wall.Seconds()
	qps := 0.0
	if secs > 0 {
		qps = float64(cfg.TotalRequests) / secs
	}

	return &Result{
		Database:         r.DB.Name(),
		WorkloadName:     "mixed",
		Category:         "mixed",
		Iterations:       cfg.TotalRequests,
		WarmupIterations: cfg.WarmupRequests,
		P50Ms:            durationToMs(Percentile(latencies, 50)),
		P95Ms:            durationToMs(Percentile(latencies, 95)),
		Failures:         failures,
		Timestamp:        time.Now().UTC(),
		QueriesPerSec:    qps,
		Concurrency:      cfg.Concurrency,
		ReadPct:          cfg.ReadPct,
		WritePct:         100 - cfg.ReadPct,
		DurationSec:      secs,
	}, nil
}

func mixedNode(dbName string, n int) db.Node {
	id := fmt.Sprintf("mixedbench-%s-%d-%d", dbName, time.Now().UnixNano(), n)
	return db.Node{
		ID:     id,
		Labels: []string{"Person"},
		Properties: map[string]any{
			"name": id,
		},
	}
}
