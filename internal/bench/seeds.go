package bench

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

// DefaultSeedNodesPath is the fixed start-node list used for every database
// and every seed-based workload (fairness: same 200 IDs everywhere).
const DefaultSeedNodesPath = "datasets/sample_seed_nodes.csv"

// LoadStartNodes reads Person ids from sample_seed_nodes.csv (column "id").
func LoadStartNodes(path string) ([]string, error) {
	if path == "" {
		path = DefaultSeedNodesPath
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open seed nodes: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReader(f))
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read seed header: %w", err)
	}
	idx := -1
	for i, h := range header {
		if strings.EqualFold(strings.TrimSpace(h), "id") {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("%s: missing id column", path)
	}

	var ids []string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read seed row: %w", err)
		}
		id := strings.TrimSpace(rec[idx])
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%s: no seed ids", path)
	}
	return ids, nil
}
