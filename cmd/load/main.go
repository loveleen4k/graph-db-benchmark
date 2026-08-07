// Command load imports nodes.csv + relationships.csv into one configured database.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"graph-benchmark/internal/db"
	"graph-benchmark/internal/loader"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	dbName := flag.String("db", "", "database key: arangodb|cognodb|falkordb|memgraph|neo4j")
	nodes := flag.String("nodes", "datasets/nodes.csv", "path to nodes.csv")
	rels := flag.String("rels", "datasets/relationships.csv", "path to relationships.csv")
	batch := flag.Int("batch", 1000, "batch size for LoadNodesBatch / LoadRelationshipsBatch")
	flag.Parse()

	if *dbName == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/load -db <name> [-nodes path] [-rels path]")
		os.Exit(2)
	}

	dbs, err := db.LoadFromEnv()
	if err != nil {
		log.Fatalf("LoadFromEnv: %v", err)
	}
	g, ok := dbs[*dbName]
	if !ok {
		log.Fatalf("database %q not configured (check .env URI)", *dbName)
	}

	if _, err := os.Stat(*nodes); err != nil {
		log.Fatalf("nodes file: %v", err)
	}
	if _, err := os.Stat(*rels); err != nil {
		log.Fatalf("rels file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()

	result, path, err := loader.LoadDataset(ctx, loader.LoadOptions{
		DB:        g,
		NodesPath: *nodes,
		RelsPath:  *rels,
		BatchSize: *batch,
	})
	if err != nil {
		log.Fatalf("load failed: %v", err)
	}

	fmt.Println("=== load complete ===")
	fmt.Printf("database:       %s\n", result.Database)
	fmt.Printf("node_count:     %d\n", result.NodeCount)
	fmt.Printf("rel_count:      %d\n", result.RelCount)
	fmt.Printf("total_seconds:  %.2f\n", result.TotalSeconds)
	fmt.Printf("nodes_per_sec:  %.2f\n", result.NodesPerSec)
	fmt.Printf("rels_per_sec:   %.2f\n", result.RelsPerSec)
	fmt.Printf("failed_batches: %d\n", result.FailedBatches)
	fmt.Printf("result file:    %s\n", path)

	// Non-zero failed batches still exit 0 so callers can inspect JSON -
	// load_all.sh treats failed_batches > 0 as a fairness stop.
	if result.FailedBatches > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d failed batches recorded\n", result.FailedBatches)
	}
}
