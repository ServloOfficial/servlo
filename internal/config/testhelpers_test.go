package config

import (
	"os"
	"testing"
)

// writeFile is the shared fixture helper for this package's tests.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
