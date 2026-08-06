package loader

import (
	"fmt"
	"os"
	"path/filepath"
)

// DatasetPath returns the path under datasets/ for a named dataset file.
func DatasetPath(name string) string {
	return filepath.Join("datasets", name)
}

// EnsureDatasetsDir creates the datasets directory if it does not exist.
func EnsureDatasetsDir() error {
	if err := os.MkdirAll("datasets", 0o755); err != nil {
		return fmt.Errorf("create datasets dir: %w", err)
	}
	return nil
}

// Clean removes a local dataset file. Download/import hooks will be added later.
func Clean(name string) error {
	path := DatasetPath(name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clean %s: %w", path, err)
	}
	return nil
}
