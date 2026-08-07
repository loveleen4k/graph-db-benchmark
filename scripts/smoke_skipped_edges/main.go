// Throwaway smoke test: confirm LoadRelationshipsBatch reports skipped_edges=2
// for 2 edges pointing at non-existent node IDs. Not part of the permanent suite.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"graph-benchmark/internal/db"
	"graph-benchmark/internal/loader"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	tmpDir := filepath.Join("tmp", "smoke_skipped_edges")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		fatal(err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
		fmt.Println("cleaned up", tmpDir)
	}()

	// Prefer real IDs from datasets/nodes.csv when present; otherwise seed five.
	nodeIDs, nodesCSV, err := resolveNodeIDs(tmpDir)
	if err != nil {
		fatal(err)
	}
	fmt.Println("using node IDs:", nodeIDs)

	relsPath := filepath.Join(tmpDir, "relationships_smoke.csv")
	if err := writeRelsCSV(relsPath, nodeIDs); err != nil {
		fatal(err)
	}
	fmt.Println("wrote", relsPath)
	if nodesCSV != "" {
		fmt.Println("seed nodes csv:", nodesCSV)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	g, err := pickDB()
	if err != nil {
		fatal(err)
	}
	fmt.Println("testing against:", g.Name())

	if err := g.Connect(ctx); err != nil {
		fatal(err)
	}
	defer g.Close(ctx)

	if err := g.CreateSchema(ctx); err != nil {
		// Memgraph/Falkor DDL often differs from Neo4j 5; continue for smoke.
		fmt.Println("CreateSchema warning (continuing):", err)
	}

	nodes := make([]db.Node, 0, len(nodeIDs))
	for i, id := range nodeIDs {
		nodes = append(nodes, db.Node{
			ID:     id,
			Labels: []string{"Person"},
			Properties: map[string]any{
				"name": fmt.Sprintf("smoke-person-%d", i),
			},
		})
	}
	if err := g.LoadNodesBatch(ctx, nodes); err != nil {
		fatal(err)
	}

	rels, err := readRelsCSV(relsPath)
	if err != nil {
		fatal(err)
	}

	stats := &loader.Stats{}
	stats.AddNodes(len(nodes))
	written, err := g.LoadRelationshipsBatch(ctx, rels)
	if err != nil {
		fatal(err)
	}
	stats.AddRelationships(len(rels), written)

	outPath, err := loader.WriteLoadResult(stats.Result(g.Name()))
	if err != nil {
		fatal(err)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		fatal(err)
	}
	fmt.Println("---", outPath, "---")
	fmt.Println(string(raw))

	var got loader.LoadResult
	if err := json.Unmarshal(raw, &got); err != nil {
		fatal(err)
	}
	if got.SkippedEdges != 2 {
		fatal(fmt.Errorf("expected skipped_edges=2, got %d (rel_count=%d)", got.SkippedEdges, got.RelCount))
	}
	if got.RelCount != 3 {
		fatal(fmt.Errorf("expected rel_count=3, got %d", got.RelCount))
	}
	fmt.Println("OK: skipped_edges == 2")

	// Remove smoke nodes/edges from the live DB so we leave no junk behind.
	if err := cleanupDB(ctx, g, nodeIDs); err != nil {
		fmt.Println("DB cleanup warning:", err)
	} else {
		fmt.Println("cleaned smoke nodes from", g.Name())
	}
	_ = os.Remove(outPath)
	fmt.Println("removed", outPath)
}

func resolveNodeIDs(tmpDir string) (ids []string, nodesCSV string, err error) {
	path := filepath.Join("datasets", "nodes.csv")
	if f, e := os.Open(path); e == nil {
		defer f.Close()
		r := csv.NewReader(f)
		rows, e := r.ReadAll()
		if e != nil {
			return nil, "", e
		}
		for i, row := range rows {
			if i == 0 && len(row) > 0 && (row[0] == "id" || row[0] == "ID") {
				continue
			}
			if len(row) == 0 || row[0] == "" {
				continue
			}
			ids = append(ids, row[0])
			if len(ids) == 5 {
				return ids, "", nil
			}
		}
		if len(ids) < 5 {
			return nil, "", fmt.Errorf("datasets/nodes.csv has fewer than 5 ids")
		}
	}

	// No real dataset yet: seed five throwaway IDs (not written into datasets/).
	ids = []string{
		"smoke-n1", "smoke-n2", "smoke-n3", "smoke-n4", "smoke-n5",
	}
	nodesCSV = filepath.Join(tmpDir, "nodes_smoke.csv")
	f, err := os.Create(nodesCSV)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"id", "name"})
	for i, id := range ids {
		_ = w.Write([]string{id, fmt.Sprintf("smoke-person-%d", i)})
	}
	w.Flush()
	return ids, nodesCSV, w.Error()
}

func writeRelsCSV(path string, ids []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"from_id", "to_id"})
	// 3 valid edges among real IDs
	_ = w.Write([]string{ids[0], ids[1]})
	_ = w.Write([]string{ids[1], ids[2]})
	_ = w.Write([]string{ids[2], ids[3]})
	// 2 edges with fake endpoints
	_ = w.Write([]string{ids[0], "does-not-exist-1"})
	_ = w.Write([]string{"does-not-exist-2", ids[1]})
	w.Flush()
	return w.Error()
}

func readRelsCSV(path string) ([]db.Relationship, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	var rels []db.Relationship
	for i, row := range rows {
		if i == 0 {
			continue
		}
		if len(row) < 2 {
			continue
		}
		rels = append(rels, db.Relationship{
			FromID: row[0],
			ToID:   row[1],
			Type:   "FOLLOWS",
		})
	}
	return rels, nil
}

func pickDB() (db.GraphDB, error) {
	// Prefer Memgraph (Bolt); FalkorDB cloud URI here is typically Redis, not Bolt.
	if uri := os.Getenv("MEMGRAPH_URI"); uri != "" {
		return db.NewMemgraph(uri, os.Getenv("MEMGRAPH_USER"), os.Getenv("MEMGRAPH_PASSWORD")), nil
	}
	if uri := os.Getenv("FALKORDB_URI"); uri != "" {
		return db.NewFalkorDB(uri, os.Getenv("FALKORDB_USER"), os.Getenv("FALKORDB_PASSWORD")), nil
	}
	if uri := os.Getenv("NEO4J_URI"); uri != "" {
		return db.NewNeo4j(uri, os.Getenv("NEO4J_USER"), os.Getenv("NEO4J_PASSWORD")), nil
	}
	return nil, fmt.Errorf("no MEMGRAPH_URI / FALKORDB_URI / NEO4J_URI set")
}

func cleanupDB(ctx context.Context, g db.GraphDB, ids []string) error {
	type idCleaner interface {
		CleanupSmokeNodes(ctx context.Context, ids []string) error
	}
	if c, ok := g.(idCleaner); ok {
		return c.CleanupSmokeNodes(ctx, ids)
	}
	type prefixCleaner interface {
		CleanupSmoketest(ctx context.Context) error
	}
	if c, ok := g.(prefixCleaner); ok {
		return c.CleanupSmoketest(ctx)
	}
	if b, ok := unwrapBolt(g); ok {
		return b.CleanupSmokeNodes(ctx, ids)
	}
	return nil
}

func unwrapBolt(g db.GraphDB) (*db.BoltGraphDB, bool) {
	switch v := g.(type) {
	case *db.Memgraph:
		return v.BoltGraphDB, true
	case *db.Neo4j:
		return v.BoltGraphDB, true
	case *db.CognoDB:
		return v.BoltGraphDB, true
	case *db.BoltGraphDB:
		return v, true
	default:
		return nil, false
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "FAIL:", err)
	os.Exit(1)
}
