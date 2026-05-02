package siteinfo

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestWorktreeWithCommittedLerdYaml_drivesEnrichedVersions exercises the
// full read path that the dashboard relies on: a worktree's checkout has a
// .lerd.yaml that we did NOT write through the lerd UI (think: the user's
// branch committed it via plain git), and the WorktreeInfo emitted by
// enrichGit must reflect those values.
//
// This is the contract relied on by:
//   - the SiteControls PHP/Node selectors in the dashboard
//   - nginx vhost generation (via config.WorktreePHPVersion)
//   - any CLI command surfacing per-worktree versions.
//
// Writing the .lerd.yaml here as raw YAML (instead of via SaveProjectConfig)
// matches what `git checkout` puts on disk byte-for-byte and proves the read
// path doesn't depend on lerd having authored the file.
func TestWorktreeWithCommittedLerdYaml_drivesEnrichedVersions(t *testing.T) {
	main, checkout := makeWorktreeFixture(t, "feat-php84")

	committedYAML := []byte(`# This file is the project's .lerd.yaml as committed in git.
php_version: "8.4"
node_version: "24"
`)
	if err := os.WriteFile(filepath.Join(checkout, ".lerd.yaml"), committedYAML, 0644); err != nil {
		t.Fatalf("write committed .lerd.yaml: %v", err)
	}

	e := &EnrichedSite{
		Path:        main,
		Domains:     []string{"acme.test"},
		PHPVersion:  "8.3",
		NodeVersion: "22",
	}
	e.enrichGit()

	if len(e.Worktrees) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(e.Worktrees))
	}
	w := e.Worktrees[0]

	if w.PHPVersion != "8.4" {
		t.Errorf("PHPVersion = %q, want %q (from committed .lerd.yaml)", w.PHPVersion, "8.4")
	}
	if !w.PHPVersionOverride {
		t.Errorf("PHPVersionOverride = false, want true so the dashboard does not show the inherited badge")
	}
	if w.NodeVersion != "24" {
		t.Errorf("NodeVersion = %q, want %q (from committed .lerd.yaml)", w.NodeVersion, "24")
	}
	if !w.NodeVersionOverride {
		t.Errorf("NodeVersionOverride = false, want true")
	}

	// Re-read the committed file to confirm we never round-tripped through
	// SaveProjectConfig (which would normalise/reformat YAML); the on-disk
	// bytes must match what was committed.
	got, err := os.ReadFile(filepath.Join(checkout, ".lerd.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var unmarshalled map[string]any
	if err := yaml.Unmarshal(got, &unmarshalled); err != nil {
		t.Fatalf("on-disk YAML no longer parses: %v", err)
	}
	if unmarshalled["php_version"] != "8.4" || unmarshalled["node_version"] != "24" {
		t.Errorf("on-disk .lerd.yaml mutated: %v", unmarshalled)
	}
}
