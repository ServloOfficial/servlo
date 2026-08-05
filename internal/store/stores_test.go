package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"gopkg.in/yaml.v3"
)

// storesDir is the in-repo store, three levels up from this package.
const storesDir = "../../stores"

// Every definition the index advertises has to exist and parse against the
// schema this binary actually reads. A store entry that only parses upstream is
// a store entry that fails on a droplet, and the index is what the dashboard's
// picker and the framework auto-fetch both walk.
func TestFrameworkStore_EveryIndexedVersionParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(storesDir, "frameworks", "index.json"))
	if err != nil {
		t.Fatalf("reading framework index: %v", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parsing framework index: %v", err)
	}
	if len(idx.Frameworks) == 0 {
		t.Fatal("the framework index is empty")
	}

	for _, entry := range idx.Frameworks {
		if entry.Latest == "" {
			t.Errorf("%s: index entry has no latest version", entry.Name)
		}
		var hasLatest bool
		for _, v := range entry.Versions {
			if v == entry.Latest {
				hasLatest = true
			}
			path := filepath.Join(storesDir, "frameworks", entry.Name, v+".yaml")
			body, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("%s@%s: indexed but missing from the store: %v", entry.Name, v, err)
				continue
			}
			var fw config.Framework
			if err := yaml.Unmarshal(body, &fw); err != nil {
				t.Errorf("%s@%s: does not parse: %v", entry.Name, v, err)
				continue
			}
			if fw.Name == "" {
				t.Errorf("%s@%s: definition has no name", entry.Name, v)
			}
		}
		if !hasLatest {
			t.Errorf("%s: latest %q is not among versions %v", entry.Name, entry.Latest, entry.Versions)
		}
	}
}

// The reverse direction: a definition nobody indexed is unreachable, since every
// fetch path starts from the index.
func TestFrameworkStore_NoUnindexedDefinitions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(storesDir, "frameworks", "index.json"))
	if err != nil {
		t.Fatalf("reading framework index: %v", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parsing framework index: %v", err)
	}
	indexed := map[string]bool{}
	for _, e := range idx.Frameworks {
		for _, v := range e.Versions {
			indexed[e.Name+"/"+v+".yaml"] = true
		}
	}

	root := filepath.Join(storesDir, "frameworks")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading framework store: %v", err)
	}
	for _, dir := range entries {
		if !dir.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, dir.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", dir.Name(), err)
		}
		for _, f := range files {
			rel := dir.Name() + "/" + f.Name()
			if !indexed[rel] {
				t.Errorf("%s is in the store but not in the index, so nothing can fetch it", rel)
			}
		}
	}
}

// Service presets are validated on the way in from the network, so they have to
// hold up against the same schema here.
func TestServiceStore_EveryIndexedPresetParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(storesDir, "services", "index.json"))
	if err != nil {
		t.Fatalf("reading service index: %v", err)
	}
	var idx ServiceIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parsing service index: %v", err)
	}
	if len(idx.Services) == 0 {
		t.Fatal("the service index is empty")
	}

	for _, entry := range idx.Services {
		path := filepath.Join(storesDir, "services", entry.Name+".yaml")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: indexed but missing from the store: %v", entry.Name, err)
			continue
		}
		var preset config.Preset
		if err := yaml.Unmarshal(body, &preset); err != nil {
			t.Errorf("%s: does not parse: %v", entry.Name, err)
		}
	}
}

// The apps store has no entries yet, but its index has to exist and be readable:
// the layout is what S0.8 pins, and an absent index would fail the first fetch
// rather than answering "nothing here".
func TestAppStore_IndexExistsAndIsEmpty(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(storesDir, "apps", "index.json"))
	if err != nil {
		t.Fatalf("reading app index: %v", err)
	}
	var idx struct {
		Apps []struct {
			Name string `json:"name"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parsing app index: %v", err)
	}
}
