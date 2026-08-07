package db

// CognoDB wraps BoltGraphDB for CognoDB's Bolt/Cypher endpoint.
// Quirk: CognoDB free-tier plans often cap concurrent Bolt sessions — keep
// connection pools small and reuse a single driver per process.
type CognoDB struct {
	*BoltGraphDB
}

// NewCognoDB returns a CognoDB client configured from environment values.
func NewCognoDB(uri, username, password string) *CognoDB {
	return &CognoDB{BoltGraphDB: NewBoltGraphDB("cognodb", uri, username, password)}
}
