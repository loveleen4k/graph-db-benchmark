package db

import (
	"context"
	"fmt"
	"os"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Memgraph is a Memgraph backend (Bolt-compatible).
type Memgraph struct {
	driver neo4j.DriverWithContext
	uri    string
}

func NewMemgraph() *Memgraph {
	return &Memgraph{uri: os.Getenv("MEMGRAPH_URI")}
}

func (m *Memgraph) Name() string { return "memgraph" }

func (m *Memgraph) Connect(ctx context.Context) error {
	password := os.Getenv("MEMGRAPH_PASSWORD")
	driver, err := neo4j.NewDriverWithContext(m.uri, neo4j.BasicAuth("", password, ""))
	if err != nil {
		return fmt.Errorf("memgraph connect: %w", err)
	}
	m.driver = driver
	return m.Ping(ctx)
}

func (m *Memgraph) Close(ctx context.Context) error {
	if m.driver == nil {
		return nil
	}
	return m.driver.Close(ctx)
}

func (m *Memgraph) Ping(ctx context.Context) error {
	if m.driver == nil {
		return fmt.Errorf("memgraph: not connected")
	}
	return m.driver.VerifyConnectivity(ctx)
}
