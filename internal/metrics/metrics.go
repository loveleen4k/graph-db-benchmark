package metrics

import (
	"sort"
	"time"
)

// Result holds aggregated timing statistics for a benchmark run.
type Result struct {
	Database   string        `json:"database"`
	Workload   string        `json:"workload"`
	Count      int           `json:"count"`
	Total      time.Duration `json:"total_ns"`
	Mean       time.Duration `json:"mean_ns"`
	P50        time.Duration `json:"p50_ns"`
	P95        time.Duration `json:"p95_ns"`
	P99        time.Duration `json:"p99_ns"`
	Min        time.Duration `json:"min_ns"`
	Max        time.Duration `json:"max_ns"`
}

// Aggregate computes percentile and summary stats from latencies in nanoseconds.
func Aggregate(database, workload string, latenciesNs []int64) Result {
	n := len(latenciesNs)
	if n == 0 {
		return Result{Database: database, Workload: workload}
	}

	sorted := make([]int64, n)
	copy(sorted, latenciesNs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total int64
	for _, v := range sorted {
		total += v
	}

	return Result{
		Database: database,
		Workload: workload,
		Count:    n,
		Total:    time.Duration(total),
		Mean:     time.Duration(total / int64(n)),
		P50:      time.Duration(percentile(sorted, 50)),
		P95:      time.Duration(percentile(sorted, 95)),
		P99:      time.Duration(percentile(sorted, 99)),
		Min:      time.Duration(sorted[0]),
		Max:      time.Duration(sorted[n-1]),
	}
}

func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
