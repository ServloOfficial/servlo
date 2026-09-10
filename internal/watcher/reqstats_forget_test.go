package watcher

import (
	"os"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/reqstats"
)

// stageRegistry writes a sites.yaml holding exactly these site names.
func stageRegistry(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	if err := os.MkdirAll(config.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "sites:\n"
	for _, n := range names {
		body += "  - name: " + n + "\n    path: /srv/" + n + "\n    domains: [" + n + ".example]\n"
	}
	if err := os.WriteFile(config.SitesFile(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Unlinking a site clears its rows from the snapshot file and the durable store,
// and the watcher still holds its rolling windows in memory. Ten seconds later
// the saver writes every site it holds back over that file, so an unlinked site
// reappears and stays until the watcher restarts.
func TestForgetUnregisteredSites_DropsASiteTheRegistryNoLongerHas(t *testing.T) {
	stageRegistry(t, "kept")
	resolve := func(h string) (string, bool) {
		switch h {
		case "kept.example":
			return "kept", true
		case "gone.example":
			return "gone", true
		}
		return "", false
	}
	agg := reqstats.New(resolve)
	agg.Record(reqstats.AccessRecord{Host: "kept.example", Method: "GET", URI: "/", RequestTime: 0.2, Status: 200})
	agg.Record(reqstats.AccessRecord{Host: "gone.example", Method: "GET", URI: "/", RequestTime: 0.3, Status: 200})

	kept := forgetUnregisteredSites(agg, agg.Snapshot())

	if len(kept) != 1 || kept[0].Site != "kept" {
		t.Errorf("snapshot = %+v, want only the registered site", kept)
	}
	if _, still := agg.SiteSnapshot("gone"); still {
		t.Error("the unregistered site is still in the aggregator, so the next tick writes it back")
	}
	if _, ok := agg.SiteSnapshot("kept"); !ok {
		t.Error("the registered site was dropped too")
	}
}

// A registry that cannot be read drops nothing: losing one read must not empty
// the snapshot of every site on the machine.
func TestForgetUnregisteredSites_KeepsEverythingWhenTheRegistryCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	if err := os.MkdirAll(config.DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.SitesFile(), []byte("sites: [this is not a registry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolve := func(string) (string, bool) { return "kept", true }
	agg := reqstats.New(resolve)
	agg.Record(reqstats.AccessRecord{Host: "kept.example", Method: "GET", URI: "/", RequestTime: 0.2, Status: 200})

	if got := forgetUnregisteredSites(agg, agg.Snapshot()); len(got) != 1 {
		t.Errorf("snapshot = %+v, want everything kept when the registry is unreadable", got)
	}
	if _, ok := agg.SiteSnapshot("kept"); !ok {
		t.Error("a site was forgotten on an unreadable registry")
	}
}
