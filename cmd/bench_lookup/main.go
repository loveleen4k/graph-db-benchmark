// Command bench_lookup runs Phase 5 Part 2: point + filtered lookup benchmarks
// on all five databases using the Phase 4 runner.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"graph-benchmark/internal/bench"
	"graph-benchmark/internal/db"
	"graph-benchmark/workloads"

	driver "github.com/arangodb/go-driver"
	"github.com/arangodb/go-driver/http"
	"github.com/joho/godotenv"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	falkordb "github.com/FalkorDB/falkordb-go/v2"
)

var dbOrder = []string{"arangodb", "cognodb", "falkordb", "memgraph", "neo4j"}

const measuredIterations = 100

func main() {
	_ = godotenv.Load()

	seeds, err := bench.LoadStartNodes(bench.DefaultSeedNodesPath)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("start nodes: %d\n", len(seeds))
	fmt.Printf("filtered lookup property: %q (indexed Person.name on all platforms)\n", workloads.FilteredLookupProperty)

	dbs, err := db.LoadFromEnv()
	if err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	fmt.Println("\n=== ensure schema / indexes ===")
	for _, name := range dbOrder {
		g, ok := dbs[name]
		if !ok {
			fatal(fmt.Errorf("missing %s", name))
		}
		if err := g.Connect(ctx); err != nil {
			fatal(fmt.Errorf("%s connect: %w", name, err))
		}
		if err := g.CreateSchema(ctx); err != nil {
			fmt.Printf("%-10s CreateSchema warning (continuing if indexes already exist): %v\n", name, err)
		} else {
			fmt.Printf("%-10s CreateSchema OK (id key + name index)\n", name)
		}
		reportIndex(ctx, name)
	}

	type row struct {
		DB, Workload string
		P50, P95     float64
		Failures     int
		Path         string
	}
	var summary []row

	for _, name := range dbOrder {
		g := dbs[name]
		fmt.Printf("\n========== %s ==========\n", name)
		runner := bench.NewRunner(g)
		for _, w := range []bench.Workload{
			workloads.NewPointLookupWorkload(),
			workloads.NewFilteredLookupWorkload(),
		} {
			fmt.Printf("--- %s ---\n", w.Name())
			res, err := runner.Run(ctx, w, bench.Config{
				Iterations:       measuredIterations,
				WarmupIterations: bench.DefaultWarmupIterations,
				StartNodes:       seeds,
			})
			if err != nil {
				fatal(fmt.Errorf("%s %s: %w", name, w.Name(), err))
			}
			path, err := bench.WriteResult(*res)
			if err != nil {
				fatal(err)
			}
			fmt.Printf("  wrote %s\n", path)
			fmt.Printf("  p50_ms=%.3f p95_ms=%.3f failures=%d\n", res.P50Ms, res.P95Ms, res.Failures)
			if res.P50Ms > 0 && res.P95Ms/res.P50Ms >= 10 {
				fmt.Printf("  FLAG: p95/p50 = %.1fx\n", res.P95Ms/res.P50Ms)
			}
			summary = append(summary, row{
				DB: name, Workload: w.Name(), Path: path,
				P50: res.P50Ms, P95: res.P95Ms, Failures: res.Failures,
			})
		}
		_ = g.Close(ctx)
	}

	// Spot-check: point lookup finds seed; name matches nodes.csv convention.
	fmt.Println("\n=== spot-check (cognodb point + filtered) ===")
	if g, ok := dbs["cognodb"]; ok {
		_ = g.Connect(ctx)
		id := seeds[0]
		found, err := g.PointLookup(ctx, id)
		fmt.Printf("point id=%s found=%v err=%v (expect name user_%s in nodes.csv)\n", id, found, err, id)
		n, err := g.IndexedLookup(ctx, "name", "user_"+id)
		fmt.Printf("filtered name=user_%s count=%d err=%v\n", id, n, err)
		_ = g.Close(ctx)
	}

	fmt.Println("\n=== SUMMARY ===")
	fmt.Printf("%-12s %-18s %10s %10s %8s\n", "database", "workload", "p50_ms", "p95_ms", "fail")
	for _, r := range summary {
		fmt.Printf("%-12s %-18s %10.3f %10.3f %8d\n", r.DB, r.Workload, r.P50, r.P95, r.Failures)
	}
	fmt.Printf("\n%d result files written\n", len(summary))
}

func reportIndex(ctx context.Context, name string) {
	switch name {
	case "neo4j", "cognodb", "memgraph":
		reportBoltIndexes(ctx, name)
	case "falkordb":
		reportFalkorIndexes(ctx)
	case "arangodb":
		reportArangoIndexes(ctx)
	}
}

func reportBoltIndexes(ctx context.Context, name string) {
	// Best-effort SHOW INDEXES where supported.
	uri, user, pass := boltCreds(name)
	d, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, pass, ""))
	if err != nil {
		fmt.Printf("%-10s index probe skip: %v\n", name, err)
		return
	}
	defer d.Close(ctx)
	sess := d.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer sess.Close(ctx)
	for _, q := range []string{
		"SHOW INDEXES YIELD labelsOrTypes, properties RETURN labelsOrTypes, properties",
		"CALL db.indexes() YIELD labelsOrTypes, properties RETURN labelsOrTypes, properties",
	} {
		res, err := sess.Run(ctx, q, nil)
		if err != nil {
			continue
		}
		var lines []string
		for res.Next(ctx) {
			lines = append(lines, fmt.Sprint(res.Record().AsMap()))
		}
		if len(lines) > 0 {
			fmt.Printf("%-10s indexes: %s\n", name, strings.Join(lines, "; "))
			return
		}
	}
	fmt.Printf("%-10s indexes: CreateSchema ensures Person(id) + Person(name) (SHOW INDEXES unavailable)\n", name)
}

func reportFalkorIndexes(ctx context.Context) {
	uri := os.Getenv("FALKORDB_URI")
	host := uri
	if i := strings.Index(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	for _, p := range []string{"falkor://", "redis://"} {
		host = strings.TrimPrefix(host, p)
	}
	client, err := falkordb.FalkorDBNew(&falkordb.ConnectionOption{
		Addr:     host,
		Username: os.Getenv("FALKORDB_USER"),
		Password: os.Getenv("FALKORDB_PASSWORD"),
	})
	if err != nil {
		fmt.Printf("falkordb   index probe skip: %v\n", err)
		return
	}
	defer client.Conn.Close()
	g := client.SelectGraph("graph_benchmark")
	res, err := g.Query("CALL db.indexes()", nil, nil)
	if err != nil {
		fmt.Printf("falkordb   indexes: CreateSchema ensures Person(id)+Person(name) (%v)\n", err)
		return
	}
	var lines []string
	for res.Next() {
		v, _ := res.Record().GetByIndex(0)
		lines = append(lines, fmt.Sprint(v))
	}
	fmt.Printf("falkordb   indexes: %v\n", lines)
}

func reportArangoIndexes(ctx context.Context) {
	conn, err := http.NewConnection(http.ConnectionConfig{Endpoints: []string{os.Getenv("ARANGODB_URI")}})
	if err != nil {
		fmt.Printf("arangodb   index probe skip: %v\n", err)
		return
	}
	client, err := driver.NewClient(driver.ClientConfig{
		Connection:     conn,
		Authentication: driver.BasicAuthentication(os.Getenv("ARANGODB_USER"), os.Getenv("ARANGODB_PASSWORD")),
	})
	if err != nil {
		fmt.Printf("arangodb   index probe skip: %v\n", err)
		return
	}
	adb, err := client.Database(ctx, "graph_benchmark")
	if err != nil {
		fmt.Printf("arangodb   index probe skip: %v\n", err)
		return
	}
	col, err := adb.Collection(ctx, "Person")
	if err != nil {
		fmt.Printf("arangodb   index probe skip: %v\n", err)
		return
	}
	idxs, err := col.Indexes(ctx)
	if err != nil {
		fmt.Printf("arangodb   index probe skip: %v\n", err)
		return
	}
	var parts []string
	for _, idx := range idxs {
		parts = append(parts, fmt.Sprintf("%s%v", idx.Name(), idx.Fields()))
	}
	fmt.Printf("arangodb   indexes: %s\n", strings.Join(parts, "; "))
}

func boltCreds(name string) (uri, user, pass string) {
	switch name {
	case "cognodb":
		return os.Getenv("COGNODB_URI"), os.Getenv("COGNODB_USER"), os.Getenv("COGNODB_PASSWORD")
	case "neo4j":
		return os.Getenv("NEO4J_URI"), os.Getenv("NEO4J_USER"), os.Getenv("NEO4J_PASSWORD")
	case "memgraph":
		return os.Getenv("MEMGRAPH_URI"), os.Getenv("MEMGRAPH_USER"), os.Getenv("MEMGRAPH_PASSWORD")
	}
	return "", "", ""
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bench_lookup FAIL:", err)
	os.Exit(1)
}
