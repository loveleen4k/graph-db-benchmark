// Command traversal_equiv cross-checks that every configured database returns
// the same 1-hop DISTINCT neighbor COUNT for the same start node(s) before any
// timed Phase 5 traversal benchmark is trusted.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"graph-benchmark/internal/bench"
	"graph-benchmark/internal/db"
	"graph-benchmark/workloads"

	"github.com/joho/godotenv"
)

var dbOrder = []string{"arangodb", "cognodb", "falkordb", "memgraph", "neo4j"}

func main() {
	_ = godotenv.Load()

	seeds, err := bench.LoadStartNodes(bench.DefaultSeedNodesPath)
	if err != nil {
		fatal(err)
	}
	// A few fixed seeds from the shared list (fairness: same IDs everywhere).
	checkSeeds := []string{seeds[0], seeds[len(seeds)/2], seeds[len(seeds)-1]}

	dbs, err := db.LoadFromEnv()
	if err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	connected := make(map[string]db.GraphDB)
	for _, name := range dbOrder {
		g, ok := dbs[name]
		if !ok {
			fatal(fmt.Errorf("missing database %q", name))
		}
		if err := g.Connect(ctx); err != nil {
			fatal(fmt.Errorf("%s connect: %w", name, err))
		}
		defer g.Close(ctx)
		connected[name] = g
	}

	fmt.Println("=== traversal semantic equivalence (1-hop counts) ===")
	allOK := true
	for _, seed := range checkSeeds {
		csvCount, err := workloads.CSVOneHopCount("datasets/relationships.csv", seed)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("\nstart_node=%s  csv_1hop_distinct=%d\n", seed, csvCount)

		counts := make(map[string]int, len(dbOrder))
		for _, name := range dbOrder {
			n, err := connected[name].Traversal(ctx, seed, 1)
			if err != nil {
				fatal(fmt.Errorf("%s Traversal(1) start=%s: %w", name, seed, err))
			}
			counts[name] = n
			fmt.Printf("  %-10s count=%d\n", name, n)
		}

		refName := dbOrder[0]
		ref := counts[refName]
		for _, name := range dbOrder[1:] {
			if counts[name] != ref {
				fmt.Printf("  MISMATCH: %s=%d vs %s=%d\n", name, counts[name], refName, ref)
				allOK = false
			}
		}
		if ref != csvCount {
			fmt.Printf("  MISMATCH vs CSV: db=%d csv=%d\n", ref, csvCount)
			allOK = false
		}
	}

	if !allOK {
		fmt.Fprintln(os.Stderr, "equivalence FAIL - fix queries before benchmarking")
		os.Exit(1)
	}
	fmt.Println("\nequivalence PASS - all databases agree with each other and CSV on 1-hop counts")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "traversal_equiv FAIL:", err)
	os.Exit(1)
}
