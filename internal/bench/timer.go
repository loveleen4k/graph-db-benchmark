// Package bench is the shared benchmark framework: timing, workloads, runner,
// and JSON results.
package bench

import "time"

// Timer is a reusable stopwatch so every workload is timed the same way.
type Timer struct {
	start time.Time
	end   time.Time
	on    bool
}

// Start begins (or restarts) the stopwatch.
func (t *Timer) Start() {
	t.start = time.Now()
	t.end = time.Time{}
	t.on = true
}

// Stop ends the stopwatch. Elapsed is frozen until the next Start.
func (t *Timer) Stop() {
	if !t.on {
		return
	}
	t.end = time.Now()
	t.on = false
}

// Elapsed returns the duration since Start. If still running, it is wall time
// so far; if Stop was called, it is the frozen interval.
func (t *Timer) Elapsed() time.Duration {
	if t.start.IsZero() {
		return 0
	}
	if t.on {
		return time.Since(t.start)
	}
	if t.end.IsZero() {
		return 0
	}
	return t.end.Sub(t.start)
}

// Reset clears the stopwatch.
func (t *Timer) Reset() {
	t.start = time.Time{}
	t.end = time.Time{}
	t.on = false
}
