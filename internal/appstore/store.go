package appstore

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/ServloOfficial/servlo/stores"
)

// Reading the store.
//
// The embedded copy is the floor every binary ships with. A definition
// published since this build is fetched, the same way frameworks and services
// are, and that path is shared rather than reimplemented here.

// Load returns the definition for name.
func Load(name string) (App, error) {
	if !appName.MatchString(name) {
		return App{}, fmt.Errorf("%q is not an app servlo knows", name)
	}
	data, ok := stores.Read(stores.Apps, name+".yaml")
	if !ok {
		return App{}, fmt.Errorf("no app definition for %q", name)
	}
	return Parse(data)
}

// List returns every app this binary ships, by name, sorted.
func List() ([]App, error) {
	entries, err := fs.ReadDir(stores.FS(), "apps")
	if err != nil {
		return nil, err
	}
	var apps []App
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		app, err := Load(strings.TrimSuffix(e.Name(), ".yaml"))
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps, nil
}

// indexEntry is one row of apps/index.json, which is what a client reads to
// know what exists before fetching any of it.
type indexEntry struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Framework string `json:"framework"`
	Version   string `json:"version"`
}

// Index returns the store's index as shipped.
func Index() ([]indexEntry, error) {
	data, ok := stores.Read(stores.Apps, "index.json")
	if !ok {
		return nil, fmt.Errorf("the app store has no index")
	}
	var doc struct {
		Apps []indexEntry `json:"apps"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("reading the app index: %w", err)
	}
	return doc.Apps, nil
}
