package loader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DatasetPath returns the path under datasets/ for a named dataset file.
func DatasetPath(name string) string {
	return filepath.Join("datasets", name)
}

// EnsureDatasetsDir creates the datasets directory if it does not exist.
func EnsureDatasetsDir() error {
	if err := os.MkdirAll("datasets", 0o755); err != nil {
		return fmt.Errorf("create datasets dir: %w", err)
	}
	return nil
}

// EnsureResultsDir creates the results directory if it does not exist.
func EnsureResultsDir() error {
	if err := os.MkdirAll("results", 0o755); err != nil {
		return fmt.Errorf("create results dir: %w", err)
	}
	return nil
}

// Clean removes a local dataset file. Download/import hooks will be added later.
func Clean(name string) error {
	path := DatasetPath(name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clean %s: %w", path, err)
	}
	return nil
}

// LoadResult is written to results/load_<db>_*.json after a dataset import.
type LoadResult struct {
	Database     string    `json:"database"`
	NodeCount    int       `json:"node_count"`
	RelCount     int       `json:"rel_count"`
	SkippedEdges int       `json:"skipped_edges"`
	Timestamp    time.Time `json:"timestamp"`
}

// Stats accumulates load counters across batches for one database.
type Stats struct {
	NodeCount    int
	RelCount     int
	SkippedEdges int
}

// AddNodes records a successful node batch of size n.
func (s *Stats) AddNodes(n int) {
	s.NodeCount += n
}

// AddRelationships records one relationship batch: written from the GraphDB
// call, skipped = submitted - written (MATCH/FILTER drops).
func (s *Stats) AddRelationships(submitted, written int) {
	if written < 0 {
		written = 0
	}
	if written > submitted {
		written = submitted
	}
	s.RelCount += written
	s.SkippedEdges += submitted - written
}

// Result builds a LoadResult snapshot for database name.
func (s *Stats) Result(database string) LoadResult {
	return LoadResult{
		Database:     database,
		NodeCount:    s.NodeCount,
		RelCount:     s.RelCount,
		SkippedEdges: s.SkippedEdges,
		Timestamp:    time.Now().UTC(),
	}
}

// WriteLoadResult writes results/load_<database>_<timestamp>.json and returns the path.
func WriteLoadResult(r LoadResult) (string, error) {
	if err := EnsureResultsDir(); err != nil {
		return "", err
	}
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now().UTC()
	}
	name := fmt.Sprintf("load_%s_%s.json", r.Database, r.Timestamp.Format("20060102T150405Z"))
	path := filepath.Join("results", name)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal load result: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write load result: %w", err)
	}
	return path, nil
}
