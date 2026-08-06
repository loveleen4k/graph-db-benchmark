package db

import (
	"context"
	"fmt"
	"os"

	driver "github.com/arangodb/go-driver"
	"github.com/arangodb/go-driver/http"
)

// ArangoDB is an ArangoDB backend using the AQL client.
type ArangoDB struct {
	client driver.Client
	uri    string
}

func NewArangoDB() *ArangoDB {
	return &ArangoDB{uri: os.Getenv("ARANGODB_URI")}
}

func (a *ArangoDB) Name() string { return "arangodb" }

func (a *ArangoDB) Connect(ctx context.Context) error {
	user := os.Getenv("ARANGODB_USER")
	password := os.Getenv("ARANGODB_PASSWORD")
	conn, err := http.NewConnection(http.ConnectionConfig{
		Endpoints: []string{a.uri},
	})
	if err != nil {
		return fmt.Errorf("arangodb connection: %w", err)
	}
	client, err := driver.NewClient(driver.ClientConfig{
		Connection:     conn,
		Authentication: driver.BasicAuthentication(user, password),
	})
	if err != nil {
		return fmt.Errorf("arangodb client: %w", err)
	}
	a.client = client
	return a.Ping(ctx)
}

func (a *ArangoDB) Close(ctx context.Context) error { return nil }

func (a *ArangoDB) Ping(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("arangodb: not connected")
	}
	_, err := a.client.Version(ctx)
	return err
}
