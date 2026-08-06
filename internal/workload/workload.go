package workload

// Workload defines a named benchmark scenario to run against a database.
type Workload interface {
	Name() string
	// Run executes the workload and returns per-operation latencies in nanoseconds.
	Run() ([]int64, error)
}
