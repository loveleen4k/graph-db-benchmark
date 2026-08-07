package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const defaultResultsDir = "results"

// Result is the shared JSON schema for every Phase 5 category
// (traversal, lookup, aggregation, mixed) and for framework demos.
// Written as results/bench_<category>_<db>_<timestamp>.json.
type Result struct {
	Database         string    `json:"database"`
	WorkloadName     string    `json:"workload_name"`
	Category         string    `json:"category"`
	Iterations       int       `json:"iterations"`
	WarmupIterations int       `json:"warmup_iterations"`
	P50Ms            float64   `json:"p50_ms"`
	P95Ms            float64   `json:"p95_ms"`
	Failures         int       `json:"failures"`
	Timestamp        time.Time `json:"timestamp"`
	// Mixed-workload extras (omitted for sequential benches).
	QueriesPerSec float64 `json:"queries_per_sec,omitempty"`
	Concurrency   int     `json:"concurrency,omitempty"`
	ReadPct       int     `json:"read_pct,omitempty"`
	WritePct      int     `json:"write_pct,omitempty"`
	DurationSec   float64 `json:"duration_seconds,omitempty"`
}

// WriteResult writes results/bench_<category>_<database>_<timestamp>.json.
func WriteResult(r Result) (string, error) {
	if err := os.MkdirAll(defaultResultsDir, 0o755); err != nil {
		return "", fmt.Errorf("create results dir: %w", err)
	}
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now().UTC()
	}
	cat := r.Category
	if cat == "" {
		cat = "bench"
	}
	name := fmt.Sprintf("bench_%s_%s_%s.json", cat, r.Database, r.Timestamp.Format("20060102T150405Z"))
	path := filepath.Join(defaultResultsDir, name)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal bench result: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write bench result: %w", err)
	}
	return path, nil
}

// Percentile returns the p-th percentile (0..100) from latencies using
// nearest-rank on a sorted copy. Callers must pass only measured samples
// (warm-up excluded).
func Percentile(latencies []time.Duration, p float64) time.Duration {
	n := len(latencies)
	if n == 0 {
		return 0
	}
	sorted := make([]time.Duration, n)
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[n-1]
	}
	// Nearest-rank: rank = ceil(p/100 * n), 1-based; convert to 0-based index.
	rank := int(math.Ceil(p / 100.0 * float64(n)))
	idx := rank - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

func durationToMs(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
