// Command sample runs BFS/snowball sampling on the SNAP Pokec edge list.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"graph-benchmark/internal/loader"
)

func main() {
	maxEdges := flag.Int("max-edges", 300000, "stop BFS after this many undirected edges")
	seedFlag := flag.Int("seed", -1, "optional seed node ID (-1 = pick among top-20 degree nodes)")
	outDir := flag.String("out-dir", "datasets", "output directory for CSV files")
	raw := flag.String("raw", "", "path to soc-pokec-relationships.txt.gz (default: datasets/raw/...)")
	flag.Parse()

	rawPath := *raw
	if rawPath == "" {
		rawPath = filepath.Join("datasets", "raw", "soc-pokec-relationships.txt.gz")
	}
	if _, err := os.Stat(rawPath); err != nil {
		log.Fatalf("raw dataset missing at %s - run scripts/download_dataset.sh first: %v", rawPath, err)
	}

	opts := loader.SampleOptions{
		RawPath:  rawPath,
		OutDir:   *outDir,
		MaxEdges: *maxEdges,
	}
	if *seedFlag >= 0 {
		s := *seedFlag
		opts.Seed = &s
	}

	res, err := loader.RunBFSSample(opts)
	if err != nil {
		log.Fatalf("sample failed: %v", err)
	}

	fmt.Println("=== BFS sample complete ===")
	fmt.Printf("seed node ID:        %d\n", res.SeedNodeID)
	fmt.Printf("nodes:               %d\n", res.NodeCount)
	fmt.Printf("directed edges:      %d\n", res.EdgeCount)
	fmt.Printf("BFS undirected edges:%d\n", res.BFSEdgeCount)
	if res.MaxEdgesReached {
		fmt.Println("stop reason:         max-edges target reached")
	} else if res.ComponentExhausted {
		fmt.Println("stop reason:         connected component exhausted before max-edges")
	} else {
		fmt.Println("stop reason:         unknown")
	}
	fmt.Printf("out-degree min/max/avg: %d / %d / %.4f\n", res.MinOutDegree, res.MaxOutDegree, res.AvgOutDegree)
	fmt.Printf("traversal seed nodes: %d (written to %s)\n",
		res.SeedNodeCount, filepath.Join(*outDir, "sample_seed_nodes.csv"))
}
