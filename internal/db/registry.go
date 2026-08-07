package db

import (
	"fmt"
	"os"
)

// LoadFromEnv constructs GraphDB implementations for every backend that has
// enough configuration in the environment. Missing databases are skipped with
// a warning so local/dev runs can target a subset of platforms.
//
// Map keys are always: cognodb, neo4j, memgraph, falkordb, arangodb.
func LoadFromEnv() (map[string]GraphDB, error) {
	out := make(map[string]GraphDB)

	if uri := os.Getenv("COGNODB_URI"); uri != "" {
		out["cognodb"] = NewCognoDB(
			uri,
			os.Getenv("COGNODB_USER"),
			os.Getenv("COGNODB_PASSWORD"),
		)
	} else {
		fmt.Fprintln(os.Stderr, "warning: skipping cognodb (COGNODB_URI not set)")
	}

	if uri := os.Getenv("NEO4J_URI"); uri != "" {
		out["neo4j"] = NewNeo4j(
			uri,
			os.Getenv("NEO4J_USER"),
			os.Getenv("NEO4J_PASSWORD"),
		)
	} else {
		fmt.Fprintln(os.Stderr, "warning: skipping neo4j (NEO4J_URI not set)")
	}

	if uri := os.Getenv("MEMGRAPH_URI"); uri != "" {
		out["memgraph"] = NewMemgraph(
			uri,
			os.Getenv("MEMGRAPH_USER"),
			os.Getenv("MEMGRAPH_PASSWORD"),
		)
	} else {
		fmt.Fprintln(os.Stderr, "warning: skipping memgraph (MEMGRAPH_URI not set)")
	}

	if uri := os.Getenv("FALKORDB_URI"); uri != "" {
		out["falkordb"] = NewFalkorDB(
			uri,
			os.Getenv("FALKORDB_USER"),
			os.Getenv("FALKORDB_PASSWORD"),
		)
	} else {
		fmt.Fprintln(os.Stderr, "warning: skipping falkordb (FALKORDB_URI not set)")
	}

	if uri := os.Getenv("ARANGODB_URI"); uri != "" {
		out["arangodb"] = NewArangoDB(
			uri,
			os.Getenv("ARANGODB_USER"),
			os.Getenv("ARANGODB_PASSWORD"),
		)
	} else {
		fmt.Fprintln(os.Stderr, "warning: skipping arangodb (ARANGODB_URI not set)")
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no databases configured: set at least one *_URI in the environment")
	}
	return out, nil
}
