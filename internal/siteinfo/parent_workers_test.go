package siteinfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// TestEnrichWorkers_keepsWorkerWhenCheckPasses pins that a worker is reported
// when its check rule matches at the site path.
func TestEnrichWorkers_keepsWorkerWhenCheckPasses(t *testing.T) {
	origUnit := unitStatusFn
	unitStatusFn = func(name string) (string, error) {
		if name == "servlo-vite-rapids" {
			return "active", nil
		}
		return "inactive", nil
	}
	defer func() { unitStatusFn = origUnit }()

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "node_modules", "vite"), 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": {
				Label:   "Vite",
				Command: "npm run dev",
				Check:   &config.FrameworkRule{File: "node_modules/vite"},
			},
		},
	}

	e := &EnrichedSite{Name: "rapids", Path: tmp}
	e.enrichWorkers(fw, true)

	var got *WorkerInfo
	for i, w := range e.FrameworkWorkers {
		if w.Name == "vite" {
			got = &e.FrameworkWorkers[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("expected vite on parent FrameworkWorkers, got %+v", e.FrameworkWorkers)
	}
	if !got.Running {
		t.Errorf("parent unit was active, want running=true: %+v", *got)
	}
}

// TestEnrichWorkers_dropsWorkerWhenCheckFails pins that the check rule gates
// visibility: a worker whose check file is absent must not leak into
// FrameworkWorkers, otherwise every Laravel project would show a vite toggle
// even without vite installed.
func TestEnrichWorkers_dropsWorkerWhenCheckFails(t *testing.T) {
	origUnit := unitStatusFn
	unitStatusFn = func(string) (string, error) { return "inactive", nil }
	defer func() { unitStatusFn = origUnit }()

	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"vite": {
				Label:   "Vite",
				Command: "npm run dev",
				Check:   &config.FrameworkRule{File: "node_modules/vite"},
			},
		},
	}

	e := &EnrichedSite{Name: "rapids", Path: t.TempDir()}
	e.enrichWorkers(fw, true)

	for _, w := range e.FrameworkWorkers {
		if w.Name == "vite" {
			t.Errorf("vite leaked in when node_modules/vite was absent: %+v", e.FrameworkWorkers)
		}
	}
}

// TestEnrichWorkers_keepsCustomWorker makes sure a custom worker the framework
// yaml ships (e.g. a "search-indexer" daemon) still reports correctly.
func TestEnrichWorkers_keepsCustomWorker(t *testing.T) {
	origUnit := unitStatusFn
	unitStatusFn = func(name string) (string, error) {
		if name == "servlo-search-indexer-rapids" {
			return "active", nil
		}
		return "inactive", nil
	}
	defer func() { unitStatusFn = origUnit }()

	fw := &config.Framework{
		Workers: map[string]config.FrameworkWorker{
			"search-indexer": {Label: "Search indexer", Command: "php artisan scout:work"},
		},
	}

	e := &EnrichedSite{Name: "rapids", Path: "/projects/rapids"}
	e.enrichWorkers(fw, true)

	if len(e.FrameworkWorkers) != 1 || e.FrameworkWorkers[0].Name != "search-indexer" {
		t.Fatalf("expected search-indexer on parent, got %+v", e.FrameworkWorkers)
	}
	if !e.FrameworkWorkers[0].Running {
		t.Errorf("parent worker status not propagated, want running=true: %+v", e.FrameworkWorkers[0])
	}
}
