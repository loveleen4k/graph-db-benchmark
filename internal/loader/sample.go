// Package loader: BFS/snowball sampling builds a connected subgraph from the
// raw SNAP edge list. A naive line-cutoff of the first N edges would often
// yield a disconnected or barely-connected scrap of the graph; multi-hop
// traversal benchmarks would then measure artificial fragmentation instead of
// real reachability. BFS from a high-degree seed grows a single connected
// component so k-hop tests reflect genuine neighborhood expansion.
package loader

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxEdges     = 300_000
	defaultSeedCandidates = 20
	defaultSeedCount    = 200
	minOutDegreeForSeed = 3
)

// SampleOptions configures BFS sampling from a gzip-compressed edge list.
type SampleOptions struct {
	RawPath  string // path to soc-pokec-relationships.txt.gz
	OutDir   string // default datasets/
	MaxEdges int    // stop BFS after this many undirected edges
	Seed     *int   // optional fixed seed node ID; nil = pick among top-degree
}

// SampleResult summarizes a completed sample run.
type SampleResult struct {
	SeedNodeID       int
	NodeCount        int
	EdgeCount        int // directed edges written to relationships.csv
	BFSEdgeCount     int // undirected edges collected during BFS
	MaxEdgesReached  bool
	ComponentExhausted bool
	MinOutDegree     int
	MaxOutDegree     int
	AvgOutDegree     float64
	SeedNodeCount    int
}

// RunBFSSample streams the gzip edge list twice: once to build an undirected
// adjacency list and BFS-sample a connected node set, then again to emit the
// original directed edges induced by that node set.
func RunBFSSample(opts SampleOptions) (*SampleResult, error) {
	if opts.RawPath == "" {
		return nil, fmt.Errorf("RawPath is required")
	}
	if opts.OutDir == "" {
		opts.OutDir = "datasets"
	}
	if opts.MaxEdges <= 0 {
		opts.MaxEdges = defaultMaxEdges
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("create out dir: %w", err)
	}

	log.Printf("pass 1: building undirected adjacency from %s", opts.RawPath)
	adj, err := buildUndirectedAdj(opts.RawPath)
	if err != nil {
		return nil, err
	}
	log.Printf("pass 1 done: %d nodes in adjacency", len(adj))

	seed, err := pickSeed(adj, opts.Seed)
	if err != nil {
		return nil, err
	}
	log.Printf("BFS seed node: %d", seed)

	nodes, bfsEdges, maxReached, exhausted := bfsSample(adj, seed, opts.MaxEdges)
	if exhausted && !maxReached {
		log.Printf("WARNING: seed %d connected component exhausted before -max-edges=%d; collected %d undirected BFS edges (continuing)",
			seed, opts.MaxEdges, bfsEdges)
	}
	log.Printf("BFS done: %d nodes, %d undirected edges (max-edges reached=%v, component exhausted=%v)",
		len(nodes), bfsEdges, maxReached, exhausted)

	// Free adjacency before second pass + CSV write.
	adj = nil

	log.Printf("pass 2: writing induced directed edges for %d sampled nodes", len(nodes))
	relPath := filepath.Join(opts.OutDir, "relationships.csv")
	nodePath := filepath.Join(opts.OutDir, "nodes.csv")
	seedPath := filepath.Join(opts.OutDir, "sample_seed_nodes.csv")

	edgeCount, outDeg, err := writeInducedCSVs(opts.RawPath, nodes, relPath, nodePath)
	if err != nil {
		return nil, err
	}

	seedIDs, err := pickTraversalSeeds(outDeg, defaultSeedCount, minOutDegreeForSeed)
	if err != nil {
		return nil, err
	}
	if err := writeSeedCSV(seedPath, seedIDs); err != nil {
		return nil, err
	}

	minD, maxD, avgD := degreeStats(outDeg, nodes)
	res := &SampleResult{
		SeedNodeID:         seed,
		NodeCount:          len(nodes),
		EdgeCount:          edgeCount,
		BFSEdgeCount:       bfsEdges,
		MaxEdgesReached:    maxReached,
		ComponentExhausted: exhausted && !maxReached,
		MinOutDegree:       minD,
		MaxOutDegree:       maxD,
		AvgOutDegree:       avgD,
		SeedNodeCount:      len(seedIDs),
	}
	return res, nil
}

func openGzip(path string) (*gzip.Reader, *os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	gr, err := gzip.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return gr, f, nil
}

func buildUndirectedAdj(path string) (map[int][]int, error) {
	gr, f, err := openGzip(path)
	if err != nil {
		return nil, fmt.Errorf("open raw: %w", err)
	}
	defer f.Close()
	defer gr.Close()

	adj := make(map[int][]int, 1<<20)
	sc := bufio.NewScanner(gr)
	// Pokec lines are short; default buffer is fine, but raise for safety.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	var lines int
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		from, to, ok := parseEdgeLine(line)
		if !ok {
			continue
		}
		adj[from] = append(adj[from], to)
		adj[to] = append(adj[to], from)
		lines++
		if lines%5_000_000 == 0 {
			log.Printf("  ... streamed %d directed edges", lines)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan raw: %w", err)
	}
	log.Printf("  streamed %d directed edges total", lines)
	return adj, nil
}

func parseEdgeLine(line string) (int, int, bool) {
	// SNAP format: "from_id\tto_id" (tab). Also accept space.
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, 0, false
	}
	from, err1 := strconv.Atoi(fields[0])
	to, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return from, to, true
}

func pickSeed(adj map[int][]int, fixed *int) (int, error) {
	if fixed != nil {
		if _, ok := adj[*fixed]; !ok {
			return 0, fmt.Errorf("seed node %d not found in graph", *fixed)
		}
		return *fixed, nil
	}
	type deg struct {
		id  int
		deg int
	}
	top := make([]deg, 0, defaultSeedCandidates)
	for id, neigh := range adj {
		d := len(neigh)
		if len(top) < defaultSeedCandidates {
			top = append(top, deg{id, d})
			if len(top) == defaultSeedCandidates {
				sort.Slice(top, func(i, j int) bool { return top[i].deg > top[j].deg })
			}
			continue
		}
		if d <= top[len(top)-1].deg {
			continue
		}
		top[len(top)-1] = deg{id, d}
		sort.Slice(top, func(i, j int) bool { return top[i].deg > top[j].deg })
	}
	if len(top) == 0 {
		return 0, fmt.Errorf("empty graph: no seed candidates")
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return top[rng.Intn(len(top))].id, nil
}

// bfsSample grows an undirected connected sample until maxEdges undirected
// edges are collected or the component is exhausted.
func bfsSample(adj map[int][]int, seed, maxEdges int) (nodes map[int]struct{}, edgeCount int, maxReached, exhausted bool) {
	nodes = map[int]struct{}{seed: {}}
	seenEdge := make(map[[2]int]struct{}, maxEdges)
	frontier := []int{seed}

	for len(frontier) > 0 && len(seenEdge) < maxEdges {
		next := make([]int, 0, len(frontier)*2)
		for _, u := range frontier {
			for _, v := range adj[u] {
				key := undirectedKey(u, v)
				if _, ok := seenEdge[key]; !ok {
					seenEdge[key] = struct{}{}
					if _, ok := nodes[v]; !ok {
						nodes[v] = struct{}{}
						next = append(next, v)
					}
					if len(seenEdge) >= maxEdges {
						return nodes, len(seenEdge), true, false
					}
				} else if _, ok := nodes[v]; !ok {
					nodes[v] = struct{}{}
					next = append(next, v)
				}
			}
		}
		frontier = next
	}

	edgeCount = len(seenEdge)
	maxReached = edgeCount >= maxEdges
	exhausted = !maxReached
	return nodes, edgeCount, maxReached, exhausted
}

func undirectedKey(a, b int) [2]int {
	if a < b {
		return [2]int{a, b}
	}
	return [2]int{b, a}
}

func writeInducedCSVs(rawPath string, nodes map[int]struct{}, relPath, nodePath string) (edgeCount int, outDeg map[int]int, err error) {
	gr, f, err := openGzip(rawPath)
	if err != nil {
		return 0, nil, fmt.Errorf("open raw for pass 2: %w", err)
	}
	defer f.Close()
	defer gr.Close()

	relFile, err := os.Create(relPath)
	if err != nil {
		return 0, nil, fmt.Errorf("create relationships.csv: %w", err)
	}
	defer relFile.Close()
	relw := bufio.NewWriterSize(relFile, 1<<20)
	if _, err := relw.WriteString("from_id,to_id\n"); err != nil {
		return 0, nil, err
	}

	outDeg = make(map[int]int, len(nodes))
	for id := range nodes {
		outDeg[id] = 0
	}

	sc := bufio.NewScanner(gr)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		from, to, ok := parseEdgeLine(line)
		if !ok {
			continue
		}
		if _, ok := nodes[from]; !ok {
			continue
		}
		if _, ok := nodes[to]; !ok {
			continue
		}
		// Preserve ORIGINAL directed edge (not undirected BFS orientation).
		if _, err := fmt.Fprintf(relw, "%d,%d\n", from, to); err != nil {
			return 0, nil, err
		}
		outDeg[from]++
		edgeCount++
	}
	if err := sc.Err(); err != nil {
		return 0, nil, fmt.Errorf("scan pass 2: %w", err)
	}
	if err := relw.Flush(); err != nil {
		return 0, nil, err
	}

	ids := make([]int, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	nodeFile, err := os.Create(nodePath)
	if err != nil {
		return 0, nil, fmt.Errorf("create nodes.csv: %w", err)
	}
	defer nodeFile.Close()
	nw := bufio.NewWriterSize(nodeFile, 1<<20)
	if _, err := nw.WriteString("id,name\n"); err != nil {
		return 0, nil, err
	}
	for _, id := range ids {
		if _, err := fmt.Fprintf(nw, "%d,user_%d\n", id, id); err != nil {
			return 0, nil, err
		}
	}
	if err := nw.Flush(); err != nil {
		return 0, nil, err
	}
	return edgeCount, outDeg, nil
}

func pickTraversalSeeds(outDeg map[int]int, want, minOut int) ([]int, error) {
	eligible := make([]int, 0)
	for id, d := range outDeg {
		if d >= minOut {
			eligible = append(eligible, id)
		}
	}
	if len(eligible) == 0 {
		return nil, fmt.Errorf("no nodes with out-degree >= %d for sample_seed_nodes", minOut)
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(eligible), func(i, j int) { eligible[i], eligible[j] = eligible[j], eligible[i] })
	if want > len(eligible) {
		want = len(eligible)
	}
	return eligible[:want], nil
}

func writeSeedCSV(path string, ids []int) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create sample_seed_nodes.csv: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	if _, err := w.WriteString("id\n"); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := fmt.Fprintf(w, "%d\n", id); err != nil {
			return err
		}
	}
	return w.Flush()
}

func degreeStats(outDeg map[int]int, nodes map[int]struct{}) (minD, maxD int, avg float64) {
	if len(nodes) == 0 {
		return 0, 0, 0
	}
	minD = int(^uint(0) >> 1)
	maxD = 0
	sum := 0
	for id := range nodes {
		d := outDeg[id]
		sum += d
		if d < minD {
			minD = d
		}
		if d > maxD {
			maxD = d
		}
	}
	avg = float64(sum) / float64(len(nodes))
	return minD, maxD, avg
}

// CountCSVLines counts data rows in a CSV (excludes header). Used by load CLI.
func CountCSVLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	n := 0
	first := true
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			if first {
				first = false
			} else if strings.TrimSpace(line) != "" {
				n++
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	return n, nil
}
