package db

import (
	"context"
	"fmt"
	"os"
)

// FalkorDB is a FalkorDB backend.
// Connection details are read from FALKORDB_URI and FALKORDB_PASSWORD.
type FalkorDB struct {
	uri string
}

func NewFalkorDB() *FalkorDB {
	return &FalkorDB{uri: os.Getenv("FALKORDB_URI")}
}

func (f *FalkorDB) Name() string { return "falkordb" }

func (f *FalkorDB) Connect(ctx context.Context) error {
	_ = os.Getenv("FALKORDB_PASSWORD")
	if f.uri == "" {
		return fmt.Errorf("falkordb: FALKORDB_URI is not set")
	}
	// Client wiring will be added with workload implementations.
	return nil
}

func (f *FalkorDB) Close(ctx context.Context) error { return nil }

func (f *FalkorDB) Ping(ctx context.Context) error {
	if f.uri == "" {
		return fmt.Errorf("falkordb: not configured")
	}
	return nil
}
