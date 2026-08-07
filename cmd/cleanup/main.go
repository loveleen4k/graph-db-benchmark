// Command cleanup wipes Person / FOLLOWS benchmark data from configured DBs
// so a failed or partial load does not pollute the next fair reload.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"graph-benchmark/internal/db"

	"github.com/joho/godotenv"
)

type clearer interface {
	ClearBenchmarkData(ctx context.Context) (int64, error)
}

var order = []string{"arangodb", "cognodb", "falkordb", "memgraph", "neo4j"}

func main() {
	_ = godotenv.Load()

	dbs, err := db.LoadFromEnv()
	if err != nil {
		log.Fatalf("LoadFromEnv: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	fmt.Println("=== clearing benchmark Person data (all configured platforms) ===")
	anyErr := false
	for _, name := range order {
		g, ok := dbs[name]
		if !ok {
			fmt.Printf("%-12s SKIP (not configured)\n", name)
			continue
		}
		if err := g.Connect(ctx); err != nil {
			fmt.Printf("%-12s FAIL connect: %v\n", name, err)
			anyErr = true
			continue
		}

		before, err := g.Aggregation(ctx)
		if err != nil {
			before = -1
			fmt.Printf("%-12s warn: count before cleanup: %v\n", name, err)
		}

		c, ok := g.(clearer)
		if !ok {
			fmt.Printf("%-12s FAIL: ClearBenchmarkData not implemented for %T\n", name, g)
			anyErr = true
			_ = g.Close(ctx)
			continue
		}

		removed, err := c.ClearBenchmarkData(ctx)
		if err != nil {
			fmt.Printf("%-12s FAIL clear: %v\n", name, err)
			anyErr = true
			_ = g.Close(ctx)
			continue
		}

		after, err := g.Aggregation(ctx)
		if err != nil {
			after = -1
		}
		_ = g.Close(ctx)

		fmt.Printf("%-12s OK  before=%d removed=%d after=%d\n", name, before, removed, after)
		if after > 0 {
			fmt.Printf("%-12s WARN: %d Person nodes remain after cleanup\n", name, after)
			anyErr = true
		}
	}

	if anyErr {
		os.Exit(1)
	}
	fmt.Println("cleanup complete")
}
