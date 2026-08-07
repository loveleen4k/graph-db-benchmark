package loader

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"graph-benchmark/internal/db"
)

const (
	defaultBatchSize = 1000
	// Abort after this many consecutive batch failures so a dead upstream
	// (e.g. 503) does not grind through millions of doomed rows. Isolated
	// failures still continue and are counted in failed_batches.
	maxConsecutiveBatchFailures = 25
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

// LoadResult is written to results/load_<db>_*.json after a dataset import.
type LoadResult struct {
	Database      string    `json:"database"`
	NodeCount     int       `json:"node_count"`
	RelCount      int       `json:"rel_count"`
	TotalSeconds  float64   `json:"total_seconds"`
	NodesPerSec   float64   `json:"nodes_per_sec"`
	RelsPerSec    float64   `json:"rels_per_sec"`
	FailedBatches int       `json:"failed_batches"`
	SkippedEdges  int       `json:"skipped_edges,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// Stats accumulates load counters across batches for one database.
type Stats struct {
	NodeCount     int
	RelCount      int
	SkippedEdges  int
	FailedBatches int
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

// Result builds a LoadResult snapshot for database name and elapsed duration.
func (s *Stats) Result(database string, elapsed time.Duration) LoadResult {
	secs := elapsed.Seconds()
	if secs <= 0 {
		secs = 1e-9
	}
	return LoadResult{
		Database:      database,
		NodeCount:     s.NodeCount,
		RelCount:      s.RelCount,
		TotalSeconds:  secs,
		NodesPerSec:   float64(s.NodeCount) / secs,
		RelsPerSec:    float64(s.RelCount) / secs,
		FailedBatches: s.FailedBatches,
		SkippedEdges:  s.SkippedEdges,
		Timestamp:     time.Now().UTC(),
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

// LoadOptions configures a CSV import into one GraphDB.
type LoadOptions struct {
	DB        db.GraphDB
	NodesPath string
	RelsPath  string
	BatchSize int
}

// LoadDataset connects, creates schema, and batch-loads nodes then relationships.
// Failed batches are logged and counted; the run continues and reports them.
func LoadDataset(ctx context.Context, opts LoadOptions) (*LoadResult, string, error) {
	if opts.DB == nil {
		return nil, "", fmt.Errorf("DB is required")
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = defaultBatchSize
	}

	start := time.Now()
	stats := &Stats{}

	if err := opts.DB.Connect(ctx); err != nil {
		return nil, "", fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = opts.DB.Close(ctx) }()

	if err := opts.DB.CreateSchema(ctx); err != nil {
		return nil, "", fmt.Errorf("CreateSchema: %w", err)
	}

	log.Printf("[%s] loading nodes from %s (batch=%d)", opts.DB.Name(), opts.NodesPath, opts.BatchSize)
	if err := loadNodes(ctx, opts, stats); err != nil {
		return nil, "", err
	}
	log.Printf("[%s] nodes loaded: %d (failed_batches so far: %d)", opts.DB.Name(), stats.NodeCount, stats.FailedBatches)

	log.Printf("[%s] loading relationships from %s", opts.DB.Name(), opts.RelsPath)
	if err := loadRels(ctx, opts, stats); err != nil {
		return nil, "", err
	}
	log.Printf("[%s] relationships loaded: %d (skipped=%d, failed_batches=%d)",
		opts.DB.Name(), stats.RelCount, stats.SkippedEdges, stats.FailedBatches)

	elapsed := time.Since(start)
	result := stats.Result(opts.DB.Name(), elapsed)
	path, err := WriteLoadResult(result)
	if err != nil {
		return &result, "", err
	}
	log.Printf("[%s] load finished in %.2fs - wrote %s", opts.DB.Name(), result.TotalSeconds, path)
	return &result, path, nil
}

func loadNodes(ctx context.Context, opts LoadOptions, stats *Stats) error {
	f, err := os.Open(opts.NodesPath)
	if err != nil {
		return fmt.Errorf("open nodes: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReaderSize(f, 1<<20))
	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("read nodes header: %w", err)
	}
	idIdx, nameIdx := columnIndex(header, "id"), columnIndex(header, "name")
	if idIdx < 0 {
		return fmt.Errorf("nodes.csv missing id column")
	}

	batch := make([]db.Node, 0, opts.BatchSize)
	consecutiveFails := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n := len(batch)
		if err := opts.DB.LoadNodesBatch(ctx, batch); err != nil {
			stats.FailedBatches++
			consecutiveFails++
			log.Printf("[%s] WARNING: node batch failed (%d rows): %v - continuing", opts.DB.Name(), n, err)
			if consecutiveFails >= maxConsecutiveBatchFailures {
				batch = batch[:0]
				return fmt.Errorf("aborting after %d consecutive node batch failures (last error: %w)", consecutiveFails, err)
			}
		} else {
			consecutiveFails = 0
			stats.AddNodes(n)
		}
		batch = batch[:0]
		if stats.NodeCount%50_000 == 0 && stats.NodeCount > 0 {
			log.Printf("[%s]   ... %d nodes", opts.DB.Name(), stats.NodeCount)
		}
		return nil
	}

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read nodes row: %w", err)
		}
		id := strings.TrimSpace(rec[idIdx])
		name := id
		if nameIdx >= 0 && nameIdx < len(rec) {
			name = strings.TrimSpace(rec[nameIdx])
		}
		batch = append(batch, db.Node{
			ID:     id,
			Labels: []string{"Person"},
			Properties: map[string]any{
				"name": name,
			},
		})
		if len(batch) >= opts.BatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func loadRels(ctx context.Context, opts LoadOptions, stats *Stats) error {
	f, err := os.Open(opts.RelsPath)
	if err != nil {
		return fmt.Errorf("open relationships: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReaderSize(f, 1<<20))
	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("read relationships header: %w", err)
	}
	fromIdx, toIdx := columnIndex(header, "from_id"), columnIndex(header, "to_id")
	if fromIdx < 0 || toIdx < 0 {
		return fmt.Errorf("relationships.csv missing from_id/to_id columns")
	}

	batch := make([]db.Relationship, 0, opts.BatchSize)
	submitted := 0
	consecutiveFails := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n := len(batch)
		written, err := opts.DB.LoadRelationshipsBatch(ctx, batch)
		if err != nil {
			stats.FailedBatches++
			consecutiveFails++
			log.Printf("[%s] WARNING: relationship batch failed (%d rows): %v - continuing", opts.DB.Name(), n, err)
			if consecutiveFails >= maxConsecutiveBatchFailures {
				batch = batch[:0]
				return fmt.Errorf("aborting after %d consecutive relationship batch failures (last error: %w)", consecutiveFails, err)
			}
		} else {
			consecutiveFails = 0
			stats.AddRelationships(n, written)
		}
		submitted += n
		batch = batch[:0]
		if submitted%100_000 == 0 && submitted > 0 {
			log.Printf("[%s]   ... %d relationships submitted (%d written)", opts.DB.Name(), submitted, stats.RelCount)
		}
		return nil
	}

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read relationships row: %w", err)
		}
		from := strings.TrimSpace(rec[fromIdx])
		to := strings.TrimSpace(rec[toIdx])
		batch = append(batch, db.Relationship{
			FromID: from,
			ToID:   to,
			Type:   "FOLLOWS",
		})
		if len(batch) >= opts.BatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func columnIndex(header []string, name string) int {
	for i, h := range header {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return i
		}
	}
	return -1
}

// LatestLoadResultPath finds the newest results/load_<db>_*.json for a database.
func LatestLoadResultPath(database string) (string, error) {
	pattern := filepath.Join("results", fmt.Sprintf("load_%s_*.json", database))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no load result for %s", database)
	}
	newest := matches[0]
	var newestMod time.Time
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.ModTime().After(newestMod) {
			newestMod = info.ModTime()
			newest = m
		}
	}
	return newest, nil
}

// ReadLoadResult parses a load result JSON file.
func ReadLoadResult(path string) (*LoadResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r LoadResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
