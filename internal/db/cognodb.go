package db

import (
	"context"
	"fmt"
	"os"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// CognoDB is a CognoDB backend (Bolt-compatible).
type CognoDB struct {
	driver neo4j.DriverWithContext
	uri    string
}

func NewCognoDB() *CognoDB {
	return &CognoDB{uri: os.Getenv("COGNODB_URI")}
}

func (c *CognoDB) Name() string { return "cognodb" }

func (c *CognoDB) Connect(ctx context.Context) error {
	password := os.Getenv("COGNODB_PASSWORD")
	driver, err := neo4j.NewDriverWithContext(c.uri, neo4j.BasicAuth("", password, ""))
	if err != nil {
		return fmt.Errorf("cognodb connect: %w", err)
	}
	c.driver = driver
	return c.Ping(ctx)
}

func (c *CognoDB) Close(ctx context.Context) error {
	if c.driver == nil {
		return nil
	}
	return c.driver.Close(ctx)
}

func (c *CognoDB) Ping(ctx context.Context) error {
	if c.driver == nil {
		return fmt.Errorf("cognodb: not connected")
	}
	return c.driver.VerifyConnectivity(ctx)
}
