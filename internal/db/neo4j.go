package db

import (
	"context"
	"fmt"
	"os"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Neo4j is a Neo4j backend.
type Neo4j struct {
	driver neo4j.DriverWithContext
	uri    string
}

func NewNeo4j() *Neo4j {
	return &Neo4j{uri: os.Getenv("NEO4J_URI")}
}

func (n *Neo4j) Name() string { return "neo4j" }

func (n *Neo4j) Connect(ctx context.Context) error {
	password := os.Getenv("NEO4J_PASSWORD")
	driver, err := neo4j.NewDriverWithContext(n.uri, neo4j.BasicAuth("neo4j", password, ""))
	if err != nil {
		return fmt.Errorf("neo4j connect: %w", err)
	}
	n.driver = driver
	return n.Ping(ctx)
}

func (n *Neo4j) Close(ctx context.Context) error {
	if n.driver == nil {
		return nil
	}
	return n.driver.Close(ctx)
}

func (n *Neo4j) Ping(ctx context.Context) error {
	if n.driver == nil {
		return fmt.Errorf("neo4j: not connected")
	}
	return n.driver.VerifyConnectivity(ctx)
}
