// Command smoketest exercises every GraphDB method against each configured
// backend using a tiny throwaway dataset, then cleans up.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"graph-benchmark/internal/db"

	"github.com/joho/godotenv"
)

const (
	statusPASS = "PASS"
	statusFAIL = "FAIL"
	statusSKIP = "SKIP"
)

var stepOrder = []string{
	"Connect",
	"Ping",
	"CreateSchema",
	"LoadNodes",
	"LoadRelationships",
	"Traversal",
	"PointLookup(exists)",
	"PointLookup(missing)",
	"IndexedLookup",
	"Aggregation",
	"WriteOne",
	"Cleanup verified",
	"Close",
}

var dbOrder = []string{"cognodb", "neo4j", "memgraph", "falkordb", "arangodb"}

type stepResult struct {
	Status string
	Detail string
}

type dbReport struct {
	Name  string
	Steps map[string]stepResult
	Lines []string
}

type smoketestCleaner interface {
	CleanupSmoketest(ctx context.Context) error
}

func main() {
	_ = godotenv.Load()

	dbs, err := db.LoadFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	reports := make([]*dbReport, 0, len(dbOrder))
	for _, name := range dbOrder {
		g, ok := dbs[name]
		if !ok {
			continue
		}
		reports = append(reports, runOne(ctx, g))
	}

	fmt.Println()
	printOverallTable(reports)

	if anyFail(reports) {
		os.Exit(1)
	}
}

func runOne(ctx context.Context, g db.GraphDB) *dbReport {
	r := &dbReport{
		Name:  g.Name(),
		Steps: make(map[string]stepResult, len(stepOrder)),
	}
	for _, s := range stepOrder {
		r.Steps[s] = stepResult{Status: statusSKIP, Detail: "not run"}
	}

	connected := false
	abort := false

	mark := func(step, status, detail string) {
		r.Steps[step] = stepResult{Status: status, Detail: detail}
		line := fmt.Sprintf("%s: %s", step, status)
		if detail != "" {
			line += " " + detail
		}
		r.Lines = append(r.Lines, line)
	}

	failAndAbort := func(step string, err error) {
		mark(step, statusFAIL, "("+err.Error()+")")
		abort = true
	}

	// 1. Connect
	if err := g.Connect(ctx); err != nil {
		failAndAbort("Connect", err)
	} else {
		mark("Connect", statusPASS, "")
		connected = true
	}

	if !abort {
		// 2. Ping
		if err := g.Ping(ctx); err != nil {
			failAndAbort("Ping", err)
		} else {
			mark("Ping", statusPASS, "")
		}
	}

	if !abort {
		// 3. CreateSchema
		if err := g.CreateSchema(ctx); err != nil {
			failAndAbort("CreateSchema", err)
		} else {
			mark("CreateSchema", statusPASS, "")
		}
	}

	if !abort {
		// 4. LoadNodesBatch
		nodes := make([]db.Node, 5)
		for i := 1; i <= 5; i++ {
			nodes[i-1] = db.Node{
				ID:     fmt.Sprintf("smoketest-%d", i),
				Labels: []string{"Person"},
				Properties: map[string]any{
					"name": fmt.Sprintf("SmokeUser%d", i),
				},
			}
		}
		if err := g.LoadNodesBatch(ctx, nodes); err != nil {
			failAndAbort("LoadNodes", err)
		} else {
			mark("LoadNodes", statusPASS, "(5 written)")
		}
	}

	if !abort {
		// 5. LoadRelationshipsBatch — chain 1->2->3->4->5
		rels := []db.Relationship{
			{FromID: "smoketest-1", ToID: "smoketest-2", Type: "FOLLOWS"},
			{FromID: "smoketest-2", ToID: "smoketest-3", Type: "FOLLOWS"},
			{FromID: "smoketest-3", ToID: "smoketest-4", Type: "FOLLOWS"},
			{FromID: "smoketest-4", ToID: "smoketest-5", Type: "FOLLOWS"},
		}
		written, err := g.LoadRelationshipsBatch(ctx, rels)
		if err != nil {
			failAndAbort("LoadRelationships", err)
		} else if written != 4 {
			failAndAbort("LoadRelationships", fmt.Errorf("expected 4 written, got %d", written))
		} else {
			mark("LoadRelationships", statusPASS, fmt.Sprintf("(%d written)", written))
		}
	}

	if !abort {
		// 6. Traversal - report count; do not hard-fail on unexpected magnitude
		n, err := g.Traversal(ctx, "smoketest-1", 2)
		if err != nil {
			failAndAbort("Traversal", err)
		} else {
			mark("Traversal", statusPASS, fmt.Sprintf("(%d nodes reached)", n))
			r.Lines[len(r.Lines)-1] = fmt.Sprintf("Traversal(2 hops): %d nodes reached", n)
			r.Steps["Traversal"] = stepResult{Status: statusPASS, Detail: fmt.Sprintf("%d nodes", n)}
		}
	}

	if !abort {
		// 7a. PointLookup exists
		found, err := g.PointLookup(ctx, "smoketest-1")
		if err != nil {
			failAndAbort("PointLookup(exists)", err)
		} else if !found {
			failAndAbort("PointLookup(exists)", fmt.Errorf("expected true, got false"))
		} else {
			mark("PointLookup(exists)", statusPASS, "")
		}
	}

	if !abort {
		// 7b. PointLookup missing
		found, err := g.PointLookup(ctx, "does-not-exist")
		if err != nil {
			failAndAbort("PointLookup(missing)", err)
		} else if found {
			failAndAbort("PointLookup(missing)", fmt.Errorf("expected false, got true"))
		} else {
			mark("PointLookup(missing)", statusPASS, "")
		}
	}

	if !abort {
		// 8. IndexedLookup
		n, err := g.IndexedLookup(ctx, "name", "SmokeUser1")
		if err != nil {
			failAndAbort("IndexedLookup", err)
		} else if n < 1 {
			failAndAbort("IndexedLookup", fmt.Errorf("expected count >= 1, got %d", n))
		} else {
			mark("IndexedLookup", statusPASS, fmt.Sprintf("(%d found)", n))
		}
	}

	if !abort {
		// 9. Aggregation — report only
		n, err := g.Aggregation(ctx)
		if err != nil {
			failAndAbort("Aggregation", err)
		} else {
			mark("Aggregation", statusPASS, fmt.Sprintf("(%d total; may include prior data)", n))
			r.Lines[len(r.Lines)-1] = fmt.Sprintf("Aggregation: %d total (may include prior data)", n)
			r.Steps["Aggregation"] = stepResult{Status: statusPASS, Detail: fmt.Sprintf("%d", n)}
		}
	}

	if !abort {
		// 10. WriteOne
		n6 := db.Node{
			ID:     "smoketest-6",
			Labels: []string{"Person"},
			Properties: map[string]any{
				"name": "SmokeUser6",
			},
		}
		if err := g.WriteOne(ctx, n6); err != nil {
			failAndAbort("WriteOne", err)
		} else {
			mark("WriteOne", statusPASS, "")
		}
	}

	// 11. CLEANUP (always attempt if connected)
	if connected {
		if err := cleanup(ctx, g); err != nil {
			mark("Cleanup verified", statusFAIL, "("+err.Error()+")")
		} else {
			found, err := g.PointLookup(ctx, "smoketest-1")
			if err != nil {
				mark("Cleanup verified", statusFAIL, "("+err.Error()+")")
			} else if found {
				mark("Cleanup verified", statusFAIL, "(smoketest-1 still present)")
			} else {
				mark("Cleanup verified", statusPASS, "")
			}
		}
	}

	// 12. Close
	if connected {
		if err := g.Close(ctx); err != nil {
			mark("Close", statusFAIL, "("+err.Error()+")")
		} else {
			mark("Close", statusPASS, "")
		}
	}

	fmt.Printf("\n=== %s ===\n", r.Name)
	for _, s := range stepOrder {
		st := r.Steps[s]
		if st.Status == statusSKIP {
			fmt.Printf("%s: %s\n", s, statusSKIP)
			continue
		}
		switch s {
		case "Traversal":
			if st.Status == statusPASS {
				n := strings.TrimSuffix(st.Detail, " nodes")
				fmt.Printf("Traversal(2 hops): %s nodes reached\n", n)
				continue
			}
		case "Aggregation":
			if st.Status == statusPASS {
				fmt.Printf("Aggregation: %s total (may include prior data)\n", st.Detail)
				continue
			}
		}
		line := fmt.Sprintf("%s: %s", s, st.Status)
		if st.Detail != "" {
			line += " " + st.Detail
		}
		fmt.Println(line)
	}

	return r
}

func cleanup(ctx context.Context, g db.GraphDB) error {
	if c, ok := g.(smoketestCleaner); ok {
		return c.CleanupSmoketest(ctx)
	}
	switch v := g.(type) {
	case *db.Memgraph:
		return v.CleanupSmoketest(ctx)
	case *db.Neo4j:
		return v.CleanupSmoketest(ctx)
	case *db.CognoDB:
		return v.CleanupSmoketest(ctx)
	case *db.FalkorDB:
		return v.CleanupSmoketest(ctx)
	case *db.ArangoDB:
		return v.CleanupSmoketest(ctx)
	case *db.BoltGraphDB:
		return v.CleanupSmoketest(ctx)
	default:
		return fmt.Errorf("no CleanupSmoketest for %T", g)
	}
}

func printOverallTable(reports []*dbReport) {
	fmt.Println("=== OVERALL SUMMARY ===")
	// Header
	cols := append([]string{"database"}, stepOrder...)
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c)
	}
	cells := make([][]string, len(reports))
	for i, r := range reports {
		row := make([]string, len(cols))
		row[0] = r.Name
		for j, step := range stepOrder {
			st := r.Steps[step].Status
			row[j+1] = st
			if len(st) > widths[j+1] {
				widths[j+1] = len(st)
			}
		}
		if len(r.Name) > widths[0] {
			widths[0] = len(r.Name)
		}
		cells[i] = row
	}

	printRow := func(row []string) {
		parts := make([]string, len(row))
		for i, v := range row {
			parts[i] = pad(v, widths[i])
		}
		fmt.Println(strings.Join(parts, " | "))
	}
	printRow(cols)
	sep := make([]string, len(cols))
	for i := range sep {
		sep[i] = strings.Repeat("-", widths[i])
	}
	printRow(sep)
	for _, row := range cells {
		printRow(row)
	}
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

func anyFail(reports []*dbReport) bool {
	for _, r := range reports {
		for _, s := range stepOrder {
			if r.Steps[s].Status == statusFAIL {
				return true
			}
		}
	}
	return false
}
