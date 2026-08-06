package db

import "context"

// Database is the common interface for graph database backends under test.
type Database interface {
	Name() string
	Connect(ctx context.Context) error
	Close(ctx context.Context) error
	Ping(ctx context.Context) error
}
