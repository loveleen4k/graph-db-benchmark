// Command chart reads results/aggregated_summary.json and writes PNG charts
// to results/charts/ using go-chart (pure Go, no gnuplot/Python).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

const (
	defaultSummary = "results/aggregated_summary.json"
	defaultOutDir  = "results/charts"
)

// Canonical platform colors — same hue for a database on every chart.
var dbColors = map[string]drawing.Color{
	"arangodb": drawing.ColorFromHex("68A063"), // Arango green
	"cognodb":  drawing.ColorFromHex("E67E22"), // orange
	"falkordb": drawing.ColorFromHex("C0392B"), // red
	"memgraph": drawing.ColorFromHex("8E44AD"), // purple
	"neo4j":    drawing.ColorFromHex("018BFF"), // Neo4j blue
}

var dbDisplay = map[string]string{
	"arangodb": "ArangoDB",
	"cognodb":  "CognoDB",
	"falkordb": "FalkorDB",
	"memgraph": "Memgraph",
	"neo4j":    "Neo4j",
}

var defaultDBOrder = []string{"arangodb", "cognodb", "falkordb", "memgraph", "neo4j"}

type lat struct {
	P50Ms float64 `json:"p50_ms"`
	P95Ms float64 `json:"p95_ms"`
}

type traversalRow struct {
	Database string `json:"database"`
	Hop1     lat    `json:"hop1"`
	Hop2     lat    `json:"hop2"`
	Hop3     lat    `json:"hop3"`
}

type lookupRow struct {
	Database string `json:"database"`
	Point    lat    `json:"point"`
	Filtered lat    `json:"filtered"`
}

type aggRow struct {
	Database string  `json:"database"`
	P50Ms    float64 `json:"p50_ms"`
	P95Ms    float64 `json:"p95_ms"`
}

type mixedRow struct {
	Database string  `json:"database"`
	QPS      float64 `json:"qps"`
}

type loadRow struct {
	Database    string  `json:"database"`
	NodesPerSec float64 `json:"nodes_per_sec"`
	RelsPerSec  float64 `json:"rels_per_sec"`
}

type variancePoint struct {
	Database string  `json:"database"`
	MinMs    float64 `json:"min_ms"`
	MeanMs   float64 `json:"mean_ms"`
	MaxMs    float64 `json:"max_ms"`
}

type varianceWorkload struct {
	Workload   string          `json:"workload"`
	Flagged    *bool           `json:"flagged"`
	Reason     string          `json:"reason"`
	ByDatabase []variancePoint `json:"by_database"`
}

type summary struct {
	Databases    []string           `json:"databases"`
	Traversals   []traversalRow     `json:"traversals"`
	Lookups      []lookupRow        `json:"lookups"`
	Aggregation  []aggRow           `json:"aggregation"`
	Mixed        []mixedRow         `json:"mixed"`
	Load         []loadRow          `json:"load"`
	HighVariance []varianceWorkload `json:"high_variance"`
}

type groupedSeries struct {
	Name   string
	Values []float64 // aligned with groups
}

type groupSpec struct {
	DBKey string
	Label string
}

func main() {
	in := flag.String("summary", defaultSummary, "path to aggregated_summary.json")
	out := flag.String("out", defaultOutDir, "output directory for PNG charts")
	flag.Parse()

	raw, err := os.ReadFile(*in)
	if err != nil {
		fatal(fmt.Errorf("read %s: %w", *in, err))
	}
	var sum summary
	if err := json.Unmarshal(raw, &sum); err != nil {
		fatal(fmt.Errorf("parse %s: %w", *in, err))
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(fmt.Errorf("create %s: %w", *out, err))
	}

	order := dbOrder(sum)
	var generated []string

	save := func(name string, err error) {
		if err != nil {
			fatal(err)
		}
		generated = append(generated, filepath.Join(*out, name))
	}

	save("traversal_latency_p50.png", writeTraversal(sum, order, false, filepath.Join(*out, "traversal_latency_p50.png")))
	save("traversal_latency_p95.png", writeTraversal(sum, order, true, filepath.Join(*out, "traversal_latency_p95.png")))
	save("lookup_latency.png", writeLookup(sum, order, filepath.Join(*out, "lookup_latency.png")))
	save("aggregation_latency.png", writeAgg(sum, order, filepath.Join(*out, "aggregation_latency.png")))
	save("mixed_workload_qps.png", writeMixed(sum, order, filepath.Join(*out, "mixed_workload_qps.png")))
	save("load_throughput.png", writeLoad(sum, order, filepath.Join(*out, "load_throughput.png")))
	save("variance_summary.png", writeVariance(sum, order, filepath.Join(*out, "variance_summary.png")))

	fmt.Println("generated charts:")
	for _, p := range generated {
		fmt.Println(p)
	}
}

func writeTraversal(sum summary, order []string, p95 bool, path string) error {
	byDB := map[string]traversalRow{}
	for _, r := range sum.Traversals {
		byDB[norm(r.Database)] = r
	}
	groups := groupsFor(order)
	hops := []string{"1-hop", "2-hop", "3-hop"}
	series := make([]groupedSeries, 3)
	for i, name := range hops {
		series[i].Name = name
		series[i].Values = make([]float64, len(groups))
		for gi, g := range groups {
			row, ok := byDB[g.DBKey]
			if !ok {
				continue
			}
			var cell lat
			switch i {
			case 0:
				cell = row.Hop1
			case 1:
				cell = row.Hop2
			default:
				cell = row.Hop3
			}
			if p95 {
				series[i].Values[gi] = cell.P95Ms
			} else {
				series[i].Values[gi] = cell.P50Ms
			}
		}
	}
	pct := "p50"
	if p95 {
		pct = "p95"
	}
	title := fmt.Sprintf("Traversal %s latency by database (1-hop / 2-hop / 3-hop)", pct)
	return renderGrouped(path, title, "Latency (ms)", groups, series, 1280, 720)
}

func writeLookup(sum summary, order []string, path string) error {
	byDB := map[string]lookupRow{}
	for _, r := range sum.Lookups {
		byDB[norm(r.Database)] = r
	}
	groups := groupsFor(order)
	series := []groupedSeries{
		{Name: "point p50", Values: make([]float64, len(groups))},
		{Name: "point p95", Values: make([]float64, len(groups))},
		{Name: "filtered p50", Values: make([]float64, len(groups))},
		{Name: "filtered p95", Values: make([]float64, len(groups))},
	}
	for gi, g := range groups {
		row := byDB[g.DBKey]
		series[0].Values[gi] = row.Point.P50Ms
		series[1].Values[gi] = row.Point.P95Ms
		series[2].Values[gi] = row.Filtered.P50Ms
		series[3].Values[gi] = row.Filtered.P95Ms
	}
	return renderGrouped(path,
		"Lookup latency by database — point vs filtered (p50 and p95)",
		"Latency (ms)", groups, series, 1400, 720)
}

func writeAgg(sum summary, order []string, path string) error {
	byDB := map[string]aggRow{}
	for _, r := range sum.Aggregation {
		byDB[norm(r.Database)] = r
	}
	groups := groupsFor(order)
	series := []groupedSeries{
		{Name: "p50", Values: make([]float64, len(groups))},
		{Name: "p95", Values: make([]float64, len(groups))},
	}
	for gi, g := range groups {
		row := byDB[g.DBKey]
		series[0].Values[gi] = row.P50Ms
		series[1].Values[gi] = row.P95Ms
	}
	return renderGrouped(path,
		"Aggregation latency by database — count all Person nodes",
		"Latency (ms)", groups, series, 1100, 680)
}

func writeMixed(sum summary, order []string, path string) error {
	byDB := map[string]mixedRow{}
	for _, r := range sum.Mixed {
		byDB[norm(r.Database)] = r
	}
	groups := groupsFor(order)
	series := []groupedSeries{{Name: "QPS", Values: make([]float64, len(groups))}}
	for gi, g := range groups {
		series[0].Values[gi] = byDB[g.DBKey].QPS
	}
	return renderGrouped(path,
		"Mixed workload throughput by database (10 clients, 80% read / 20% write)",
		"Throughput (qps)", groups, series, 1000, 640)
}

func writeLoad(sum summary, order []string, path string) error {
	byDB := map[string]loadRow{}
	for _, r := range sum.Load {
		byDB[norm(r.Database)] = r
	}
	groups := groupsFor(order)
	series := []groupedSeries{
		{Name: "nodes/sec", Values: make([]float64, len(groups))},
		{Name: "rels/sec", Values: make([]float64, len(groups))},
	}
	for gi, g := range groups {
		row := byDB[g.DBKey]
		series[0].Values[gi] = row.NodesPerSec
		series[1].Values[gi] = row.RelsPerSec
	}
	return renderGrouped(path,
		"Load throughput by database — nodes/sec and relationships/sec",
		"Throughput (per second)", groups, series, 1100, 680)
}

func writeVariance(sum summary, order []string, path string) error {
	var flagged []varianceWorkload
	for _, w := range sum.HighVariance {
		if w.Flagged != nil && !*w.Flagged {
			continue
		}
		if len(w.ByDatabase) == 0 {
			continue
		}
		flagged = append(flagged, w)
	}
	if len(flagged) == 0 {
		return renderGrouped(path,
			"Variance summary — no high-variance workloads flagged",
			"Latency (ms)", groupsFor(order),
			[]groupedSeries{{Name: "n/a", Values: make([]float64, len(order))}},
			1000, 640)
	}

	// One cluster per database; if several flagged workloads exist, flatten
	// labels as "workload / db" so min/mean/max still group together.
	var groups []groupSpec
	mins := []float64{}
	means := []float64{}
	maxs := []float64{}
	titleBits := []string{}

	if len(flagged) == 1 {
		w := flagged[0]
		titleBits = append(titleBits, w.Workload)
		byDB := map[string]variancePoint{}
		for _, p := range w.ByDatabase {
			byDB[norm(p.Database)] = p
		}
		for _, key := range order {
			p, ok := byDB[key]
			if !ok {
				continue
			}
			groups = append(groups, groupSpec{DBKey: key, Label: displayName(key)})
			mins = append(mins, p.MinMs)
			means = append(means, p.MeanMs)
			maxs = append(maxs, p.MaxMs)
		}
		title := fmt.Sprintf("High-variance: %s — min / mean / max latency across runs", w.Workload)
		if w.Reason != "" {
			title = fmt.Sprintf("High-variance: %s — min / mean / max (p50 typical, p95 tail)", w.Workload)
		}
		return renderGrouped(path, title, "Latency (ms)", groups, []groupedSeries{
			{Name: "min", Values: mins},
			{Name: "mean", Values: means},
			{Name: "max", Values: maxs},
		}, 1200, 720)
	}

	for _, w := range flagged {
		titleBits = append(titleBits, w.Workload)
		for _, p := range w.ByDatabase {
			key := norm(p.Database)
			groups = append(groups, groupSpec{
				DBKey: key,
				Label: displayName(key) + " / " + w.Workload,
			})
			mins = append(mins, p.MinMs)
			means = append(means, p.MeanMs)
			maxs = append(maxs, p.MaxMs)
		}
	}
	title := "High-variance workloads — min / mean / max latency by database"
	_ = titleBits
	return renderGrouped(path, title, "Latency (ms)", groups, []groupedSeries{
		{Name: "min", Values: mins},
		{Name: "mean", Values: means},
		{Name: "max", Values: maxs},
	}, 1400, 720)
}

func renderGrouped(path, title, yName string, groups []groupSpec, series []groupedSeries, width, height int) error {
	if len(groups) == 0 {
		return fmt.Errorf("%s: no database groups to plot", path)
	}
	bars := make([]chart.Value, 0, len(groups)*len(series))
	maxV := 0.0
	for gi, g := range groups {
		for si, s := range series {
			v := 0.0
			if gi < len(s.Values) {
				v = s.Values[gi]
			}
			if v > maxV {
				maxV = v
			}
			label := g.Label
			if len(series) > 1 {
				label = s.Name
				if si == 0 {
					label = g.Label
				}
			}
			col := colorFor(g.DBKey)
			fill := shade(col, si, len(series))
			bars = append(bars, chart.Value{
				Label: label,
				Value: v,
				Style: chart.Style{
					FillColor:   fill,
					StrokeColor: col,
					StrokeWidth: 1.2,
				},
			})
		}
	}
	if maxV <= 0 {
		maxV = 1
	}

	leftPad := 70
	if strings.Contains(strings.ToLower(yName), "per second") {
		leftPad = 84
	}

	graph := chart.BarChart{
		Title: title,
		TitleStyle: chart.Style{
			FontSize:  13,
			FontColor: drawing.ColorBlack,
			Padding:   chart.Box{Top: 8},
		},
		Width:      width,
		Height:     height,
		BarWidth:   barWidthFor(len(bars), width),
		BarSpacing: 8,
		Background: chart.Style{
			FillColor: drawing.ColorWhite,
			Padding: chart.Box{
				Top:    48,
				Left:   leftPad,
				Right:  28,
				Bottom: 88,
			},
		},
		XAxis: chart.Style{
			FontSize:  9,
			FontColor: drawing.ColorBlack,
		},
		YAxis: chart.YAxis{
			Name:     yName,
			AxisType: chart.YAxisSecondary, // go-chart: secondary = left axis
			NameStyle: chart.Style{
				FontSize:            11,
				FontColor:           drawing.ColorBlack,
				TextRotationDegrees: 90,
			},
			Style: chart.Style{FontSize: 10, FontColor: drawing.ColorBlack},
			Range: &chart.ContinuousRange{Min: 0, Max: maxV * 1.15},
			GridMajorStyle: chart.Style{
				StrokeColor: drawing.Color{R: 220, G: 220, B: 220, A: 255},
				StrokeWidth: 1,
			},
			ValueFormatter: func(v interface{}) string {
				f, _ := v.(float64)
				if f >= 100 {
					return fmt.Sprintf("%.0f", f)
				}
				return fmt.Sprintf("%.1f", f)
			},
		},
		Bars: bars,
		Elements: []chart.Renderable{
			legendRenderable(groups, series),
		},
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	if err := graph.Render(chart.PNG, f); err != nil {
		return fmt.Errorf("render %s: %w", path, err)
	}
	return nil
}

func legendRenderable(groups []groupSpec, series []groupedSeries) chart.Renderable {
	return func(r chart.Renderer, cb chart.Box, defaults chart.Style) {
		seen := map[string]bool{}
		var dbs []string
		for _, g := range groups {
			if seen[g.DBKey] {
				continue
			}
			seen[g.DBKey] = true
			dbs = append(dbs, g.DBKey)
		}

		textStyle := chart.Style{
			Font:      defaults.GetFont(),
			FontSize:  10,
			FontColor: drawing.ColorBlack,
		}
		noteStyle := chart.Style{
			Font:      defaults.GetFont(),
			FontSize:  8,
			FontColor: drawing.Color{R: 70, G: 70, B: 70, A: 255},
		}

		rowH := 16
		pad := 8
		boxW := 148
		extra := 0
		if len(series) > 1 {
			extra = 10 + 12*(len(series)+1)
		}
		boxH := pad*2 + 14 + rowH*len(dbs) + extra
		left := cb.Right - boxW - 6
		if left < cb.Left+6 {
			left = cb.Left + 6
		}
		top := cb.Top + 6

		chart.Draw.Box(r, chart.Box{
			Top: top, Left: left, Right: left + boxW, Bottom: top + boxH,
		}, chart.Style{
			FillColor:   drawing.Color{R: 255, G: 255, B: 255, A: 230},
			StrokeColor: drawing.Color{R: 180, G: 180, B: 180, A: 255},
			StrokeWidth: 1,
		})

		y := top + pad + 12
		chart.Draw.Text(r, "Databases", left+pad, y, textStyle)
		for _, key := range dbs {
			y += rowH
			col := colorFor(key)
			chart.Draw.Box(r, chart.Box{
				Top: y - 10, Left: left + pad, Right: left + pad + 12, Bottom: y + 1,
			}, chart.Style{FillColor: col, StrokeColor: col, StrokeWidth: 1})
			chart.Draw.Text(r, displayName(key), left+pad+18, y, textStyle)
		}
		if len(series) > 1 {
			y += 16
			chart.Draw.Text(r, "Bar order (left to right)", left+pad, y, noteStyle)
			for _, s := range series {
				y += 12
				chart.Draw.Text(r, "- "+s.Name, left+pad, y, noteStyle)
			}
		}
	}
}

func barWidthFor(n, width int) int {
	if n <= 0 {
		return 40
	}
	w := (width - 280) / (n + 2)
	if w > 48 {
		return 48
	}
	if w < 12 {
		return 12
	}
	return w
}

func shade(c drawing.Color, i, n int) drawing.Color {
	if n <= 1 {
		return c
	}
	// Mix toward white as i increases, keeping hue (database identity).
	t := 0.12 * float64(i)
	if t > 0.45 {
		t = 0.45
	}
	return drawing.Color{
		R: uint8(float64(c.R)*(1-t) + 255*t),
		G: uint8(float64(c.G)*(1-t) + 255*t),
		B: uint8(float64(c.B)*(1-t) + 255*t),
		A: 255,
	}
}

func groupsFor(order []string) []groupSpec {
	out := make([]groupSpec, 0, len(order))
	for _, k := range order {
		out = append(out, groupSpec{DBKey: k, Label: displayName(k)})
	}
	return out
}

func dbOrder(sum summary) []string {
	if len(sum.Databases) > 0 {
		out := make([]string, 0, len(sum.Databases))
		for _, d := range sum.Databases {
			out = append(out, norm(d))
		}
		return out
	}
	return append([]string{}, defaultDBOrder...)
}

func colorFor(key string) drawing.Color {
	if c, ok := dbColors[norm(key)]; ok {
		return c
	}
	return drawing.Color{R: 120, G: 120, B: 120, A: 255}
}

func displayName(key string) string {
	if n, ok := dbDisplay[norm(key)]; ok {
		return n
	}
	return key
}

func norm(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "chart: %v\n", err)
	os.Exit(1)
}
