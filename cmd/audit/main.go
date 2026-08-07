// Command audit_sample validates BFS sample CSVs and load result JSONs.
package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	nodesPath := flagOr("nodes", "datasets/nodes.csv")
	relsPath := flagOr("rels", "datasets/relationships.csv")
	seedsPath := flagOr("seeds", "datasets/sample_seed_nodes.csv")
	resultsDir := flagOr("results", "results")

	ok := true
	check := func(name string, pass bool, detail string) {
		status := "PASS"
		if !pass {
			status = "FAIL"
			ok = false
		}
		fmt.Printf("[%s] %s: %s\n", status, name, detail)
	}

	nodeSet, err := loadIDSet(nodesPath, "id")
	if err != nil {
		fatal(err)
	}
	check("nodes.csv non-empty", len(nodeSet) > 0, fmt.Sprintf("%d nodes", len(nodeSet)))

	outDeg := map[string]int{}
	orphanRels := 0
	relCount := 0
	f, err := os.Open(relsPath)
	if err != nil {
		fatal(err)
	}
	r := csv.NewReader(bufio.NewReaderSize(f, 1<<20))
	header, err := r.Read()
	if err != nil {
		fatal(err)
	}
	fromIdx, toIdx := col(header, "from_id"), col(header, "to_id")
	if fromIdx < 0 || toIdx < 0 {
		fatal(fmt.Errorf("relationships.csv missing from_id/to_id"))
	}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			fatal(err)
		}
		from, to := strings.TrimSpace(rec[fromIdx]), strings.TrimSpace(rec[toIdx])
		relCount++
		outDeg[from]++
		if !nodeSet[from] || !nodeSet[to] {
			orphanRels++
		}
	}
	_ = f.Close()
	check("no orphaned relationship endpoints", orphanRels == 0, fmt.Sprintf("%d orphan edges of %d", orphanRels, relCount))
	check("relationships non-zero", relCount > 0, fmt.Sprintf("%d edges", relCount))

	sum := 0
	maxD := 0
	for _, d := range outDeg {
		sum += d
		if d > maxD {
			maxD = d
		}
	}
	// Nodes with no out-edge still count toward avg over all nodes.
	avg := float64(sum) / float64(len(nodeSet))
	check("avg out-degree reasonable", avg >= 0.5 && avg < 500, fmt.Sprintf("avg=%.4f max=%d", avg, maxD))

	seeds, err := loadIDList(seedsPath, "id")
	if err != nil {
		fatal(err)
	}
	check("seed list size", len(seeds) >= 50 && len(seeds) <= 250, fmt.Sprintf("%d seeds", len(seeds)))
	missingSeed := 0
	lowDegSeed := 0
	for _, id := range seeds {
		if !nodeSet[id] {
			missingSeed++
		}
		if outDeg[id] < 3 {
			lowDegSeed++
		}
	}
	check("seeds exist in nodes.csv", missingSeed == 0, fmt.Sprintf("%d missing", missingSeed))
	check("seeds out-degree >= 3", lowDegSeed == 0, fmt.Sprintf("%d below threshold", lowDegSeed))

	platforms := []string{"arangodb", "cognodb", "falkordb", "memgraph", "neo4j"}
	type lr struct {
		Database      string  `json:"database"`
		NodeCount     int     `json:"node_count"`
		RelCount      int     `json:"rel_count"`
		TotalSeconds  float64 `json:"total_seconds"`
		NodesPerSec   float64 `json:"nodes_per_sec"`
		RelsPerSec    float64 `json:"rels_per_sec"`
		FailedBatches int     `json:"failed_batches"`
	}
	var results []lr
	for _, p := range platforms {
		path, err := latest(resultsDir, p)
		if err != nil {
			check("load result exists: "+p, false, err.Error())
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			check("load result readable: "+p, false, err.Error())
			continue
		}
		var row lr
		if err := json.Unmarshal(data, &row); err != nil {
			check("load result JSON: "+p, false, err.Error())
			continue
		}
		results = append(results, row)
		check("load counts non-zero: "+p, row.NodeCount > 0 && row.RelCount > 0,
			fmt.Sprintf("nodes=%d rels=%d secs=%.2f failed=%d file=%s", row.NodeCount, row.RelCount, row.TotalSeconds, row.FailedBatches, filepath.Base(path)))
		check("failed_batches zero: "+p, row.FailedBatches == 0, fmt.Sprintf("%d", row.FailedBatches))
	}

	if len(results) == 5 {
		n0, r0 := results[0].NodeCount, results[0].RelCount
		match := true
		identicalPaste := true
		for i := 1; i < len(results); i++ {
			if results[i].NodeCount != n0 || results[i].RelCount != r0 {
				match = false
			}
			if results[i].TotalSeconds != results[0].TotalSeconds ||
				results[i].NodesPerSec != results[0].NodesPerSec {
				identicalPaste = false
			}
		}
		check("node/rel counts match across all 5", match, fmt.Sprintf("nodes=%d rels=%d", n0, r0))
		check("timing fields not identical copy-paste", !identicalPaste || results[0].TotalSeconds == 0,
			"timings differ across platforms (expected)")
		check("loaded counts match CSV", n0 == len(nodeSet) && r0 == relCount,
			fmt.Sprintf("csv nodes=%d rels=%d vs load nodes=%d rels=%d", len(nodeSet), relCount, n0, r0))
	}

	fmt.Println()
	if !ok {
		fmt.Println("AUDIT FAILED")
		os.Exit(1)
	}
	fmt.Println("AUDIT PASSED")
}

func flagOr(name, def string) string {
	prefix := "-" + name + "="
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, prefix) {
			return strings.TrimPrefix(a, prefix)
		}
		if a == "-"+name && len(os.Args) > 1 {
			// unsupported; keep simple
		}
	}
	_ = name
	return def
}

func loadIDSet(path, colName string) (map[string]bool, error) {
	list, err := loadIDList(path, colName)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(list))
	for _, id := range list {
		m[id] = true
	}
	return m, nil
}

func loadIDList(path, colName string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(bufio.NewReader(f))
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := col(header, colName)
	if idx < 0 {
		return nil, fmt.Errorf("%s missing column %s", path, colName)
	}
	var out []string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, strings.TrimSpace(rec[idx]))
	}
	return out, nil
}

func col(header []string, name string) int {
	for i, h := range header {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return i
		}
	}
	return -1
}

func latest(dir, db string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, fmt.Sprintf("load_%s_*.json", db)))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no load_%s_*.json in %s", db, dir)
	}
	newest := matches[0]
	var newestMod int64
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		t := info.ModTime().UnixNano()
		if t >= newestMod {
			newestMod = t
			newest = m
		}
	}
	return newest, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "audit error:", err)
	os.Exit(2)
}
