package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Browser mocks and driver containers are things CI runs, not things a server
// runs. Two of them shipped in the store — a Stripe API mock and Selenium —
// which is the class of thing S0.5 and S0.6 removed everywhere else.
//
// This asserts the property rather than the names: a preset is disqualified by
// declaring itself a testing tool, so a new one cannot arrive under a name
// nobody thought to forbid.
func TestNoTestingPresetsShip(t *testing.T) {
	dir := filepath.Join("..", "..", "stores", "services")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the service store: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			if strings.TrimSpace(line) == "category: testing" {
				t.Errorf("%s declares category: testing — a testing tool has no production role, "+
					"and shipping one puts it on every install", e.Name())
			}
		}
	}
}
