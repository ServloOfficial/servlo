package appstore

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// No app's name may appear in this package's Go source.
//
// The law is CLAUDE.md §2, and the skill that documents this store says plainly
// that the surface scan does not catch it and review does. Review is a person
// remembering, which is not a mechanism, so this is one: every app the store
// ships is looked for in every Go file here, and a match fails the build.
//
// It is scoped to this package on purpose. The engine is where the temptation
// lives, because the engine is what an app's awkward requirement would be
// special-cased into.
func TestNoAppNameAppearsInGo(t *testing.T) {
	apps, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) == 0 {
		t.Fatal("no apps to check against, so this test proves nothing")
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		// This file names every app by construction.
		if e.Name() == "law_test.go" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, app := range apps {
			// No word boundaries. The shapes this law is actually about are
			// wordpress_config and wordpressInstall, and an underscore is a word
			// character, so \b would let the exact thing being forbidden through.
			// The first version of this test did, and a mutation proved it.
			pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(app.Name))
			for i, line := range strings.Split(string(body), "\n") {
				if pattern.MatchString(line) {
					t.Errorf("%s:%d names the app %q: %s\n\nAn app's requirement belongs in its YAML, or in a general field named for what it does.",
						e.Name(), i+1, app.Name, strings.TrimSpace(line))
				}
			}
		}
	}
}
