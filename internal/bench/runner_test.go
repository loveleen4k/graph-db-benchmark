package bench

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"graph-benchmark/internal/db"
)

// stubDB implements db.GraphDB with no-op methods for unit tests.
type stubDB struct {
	name string
}

func (s *stubDB) Name() string                                       { return s.name }
func (s *stubDB) Connect(context.Context) error                      { return nil }
func (s *stubDB) Close(context.Context) error                        { return nil }
func (s *stubDB) Ping(context.Context) error                         { return nil }
func (s *stubDB) CreateSchema(context.Context) error                 { return nil }
func (s *stubDB) LoadNodesBatch(context.Context, []db.Node) error    { return nil }
func (s *stubDB) LoadRelationshipsBatch(context.Context, []db.Relationship) (int, error) {
	return 0, nil
}
func (s *stubDB) Traversal(context.Context, string, int) (int, error) { return 0, nil }
func (s *stubDB) PointLookup(context.Context, string) (bool, error)   { return true, nil }
func (s *stubDB) IndexedLookup(context.Context, string, string) (int, error) {
	return 0, nil
}
func (s *stubDB) Aggregation(context.Context) (int64, error) { return 0, nil }
func (s *stubDB) WriteOne(context.Context, db.Node) error    { return nil }

func TestTimerElapsed(t *testing.T) {
	var tm Timer
	tm.Start()
	time.Sleep(5 * time.Millisecond)
	tm.Stop()
	elapsed := tm.Elapsed()
	if elapsed < 5*time.Millisecond {
		t.Fatalf("elapsed %v too small", elapsed)
	}
	// Frozen after Stop.
	time.Sleep(3 * time.Millisecond)
	if tm.Elapsed() != elapsed {
		t.Fatalf("elapsed changed after Stop: %v vs %v", tm.Elapsed(), elapsed)
	}
}

func TestPercentileSortedNearestRank(t *testing.T) {
	// 100 samples: 1ms..100ms
	lat := make([]time.Duration, 100)
	for i := 0; i < 100; i++ {
		lat[i] = time.Duration(i+1) * time.Millisecond
	}
	p50 := Percentile(lat, 50)
	p95 := Percentile(lat, 95)
	// nearest-rank ceil(0.50*100)=50 -> index 49 -> 50ms
	if p50 != 50*time.Millisecond {
		t.Fatalf("p50=%v want 50ms", p50)
	}
	// ceil(0.95*100)=95 -> index 94 -> 95ms
	if p95 != 95*time.Millisecond {
		t.Fatalf("p95=%v want 95ms", p95)
	}
}

func TestWarmupExcludedFromPercentiles(t *testing.T) {
	var calls atomic.Int64
	w := WorkloadFunc{
		OpName:     "slow_then_fast",
		OpCategory: "framework_check",
		UseSeed:    false,
		Fn: func(context.Context, db.GraphDB, string) error {
			n := calls.Add(1)
			// First 5 calls (warm-up) sleep longer; measured calls are fast.
			if n <= 5 {
				time.Sleep(20 * time.Millisecond)
			} else {
				time.Sleep(2 * time.Millisecond)
			}
			return nil
		},
	}

	r := NewRunner(&stubDB{name: "stub"})
	res, err := r.Run(context.Background(), w, Config{
		Iterations:       10,
		WarmupIterations: 5,
		DisableWarmup:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.WarmupIterations != 5 {
		t.Fatalf("warmup=%d", res.WarmupIterations)
	}
	if res.Iterations != 10 {
		t.Fatalf("iterations=%d", res.Iterations)
	}
	// If warm-up (20ms) leaked into stats, p50 would be well above ~10ms.
	if res.P50Ms > 15 {
		t.Fatalf("p50_ms=%.2f looks like warm-up leaked into measured stats", res.P50Ms)
	}
	if res.P95Ms > 15 {
		t.Fatalf("p95_ms=%.2f looks like warm-up leaked into measured stats", res.P95Ms)
	}
	if calls.Load() != 15 {
		t.Fatalf("calls=%d want 15 (5 warmup + 10 measured)", calls.Load())
	}
}

func TestFailuresCountedNotDropped(t *testing.T) {
	var calls atomic.Int64
	w := WorkloadFunc{
		OpName:     "flaky",
		OpCategory: "framework_check",
		UseSeed:    false,
		Fn: func(context.Context, db.GraphDB, string) error {
			n := calls.Add(1)
			if n%2 == 0 {
				return errors.New("boom")
			}
			return nil
		},
	}
	r := NewRunner(&stubDB{name: "stub"})
	res, err := r.Run(context.Background(), w, Config{
		Iterations:    10,
		DisableWarmup: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failures != 5 {
		t.Fatalf("failures=%d want 5", res.Failures)
	}
	if res.Iterations != 10 {
		t.Fatalf("iterations=%d", res.Iterations)
	}
	// Successes still produce percentiles.
	if res.P50Ms < 0 {
		t.Fatal("p50 should be set for successful ops")
	}
}

func TestStartNodeRotation(t *testing.T) {
	seen := make([]string, 0, 6)
	w := WorkloadFunc{
		OpName:     "seeded",
		OpCategory: "framework_check",
		UseSeed:    true,
		Fn: func(_ context.Context, _ db.GraphDB, start string) error {
			seen = append(seen, start)
			return nil
		},
	}
	r := NewRunner(&stubDB{name: "stub"})
	_, err := r.Run(context.Background(), w, Config{
		Iterations:    4,
		DisableWarmup: true,
		StartNodes:    []string{"a", "b", "c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c", "a"}
	if len(seen) != len(want) {
		t.Fatalf("seen=%v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("seen=%v want %v", seen, want)
		}
	}
}
