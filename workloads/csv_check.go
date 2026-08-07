package workloads

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

// CSVOneHopCount returns the number of distinct outbound neighbors of startID
// in relationships.csv (from_id -> to_id). Used to sanity-check 1-hop queries.
func CSVOneHopCount(relsPath, startID string) (int, error) {
	f, err := os.Open(relsPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReaderSize(f, 1<<20))
	header, err := r.Read()
	if err != nil {
		return 0, err
	}
	fromIdx, toIdx := -1, -1
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "from_id":
			fromIdx = i
		case "to_id":
			toIdx = i
		}
	}
	if fromIdx < 0 || toIdx < 0 {
		return 0, fmt.Errorf("%s: need from_id and to_id columns", relsPath)
	}

	seen := make(map[string]struct{})
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		from := strings.TrimSpace(rec[fromIdx])
		if from != startID {
			continue
		}
		to := strings.TrimSpace(rec[toIdx])
		if to == startID {
			continue // exclude self-loops from distinct-neighbor count
		}
		seen[to] = struct{}{}
	}
	return len(seen), nil
}
