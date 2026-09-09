package config

import (
	"path/filepath"
	"testing"
)

// A custom-container site need not be on a framework at all, and the workers
// beside it live in its own .servlo.yaml. Resolving its framework has to find
// them, because everything downstream of this call reads fw.Workers.
func TestFrameworkForSite_ACustomContainerWithoutAFrameworkStillHasItsWorkers(t *testing.T) {
	dir := t.TempDir()
	writeCustomWorkers(t, dir, map[string]FrameworkWorker{
		"ingest": {Command: "node ingest.js"},
	})
	site := &Site{Name: "acme", Path: dir, ContainerPort: 8080}

	fw, ok := FrameworkForSite(site)
	if !ok {
		t.Fatal("a custom-container site with custom_workers resolved to no framework, so nothing downstream can see its workers")
	}
	if _, has := fw.Workers["ingest"]; !has {
		t.Errorf("workers = %v, want the site's own ingest worker", fw.Workers)
	}
}

// The workers come out of a file inside the site's repository, so a host worker
// among them is a command from a git pull asking to run on the server. The
// host-execution gate reads ProjectOrigin, and a fallback that leaves it false
// is a fallback that skips the consent prompt the same worker gets everywhere
// else.
func TestFrameworkForSite_MarksTheSitesOwnWorkersUntrusted(t *testing.T) {
	dir := t.TempDir()
	writeCustomWorkers(t, dir, map[string]FrameworkWorker{
		"tailer": {Command: "tail -f /var/log/syslog", Host: true},
	})
	site := &Site{Name: "acme", Path: dir, ContainerPort: 8080}

	fw, ok := FrameworkForSite(site)
	if !ok {
		t.Fatal("resolved to no framework")
	}
	if !fw.Workers["tailer"].ProjectOrigin {
		t.Error("a host worker read out of the site's own .servlo.yaml is not marked untrusted, so it runs on the host without asking")
	}
}

// A site that is not a custom container has no such fallback: its workers come
// from the framework it is on, and inventing one would run a project's file as
// though servlo had reviewed it.
func TestFrameworkForSite_OnlyACustomContainerGetsTheFallback(t *testing.T) {
	dir := t.TempDir()
	writeCustomWorkers(t, dir, map[string]FrameworkWorker{"ingest": {Command: "node ingest.js"}})

	if _, ok := FrameworkForSite(&Site{Name: "acme", Path: dir}); ok {
		t.Error("a site with no framework and no container resolved to one anyway")
	}
}

func TestFrameworkForSite_NoWorkersMeansNoFramework(t *testing.T) {
	dir := t.TempDir()
	if err := SaveProjectConfig(dir, &ProjectConfig{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := FrameworkForSite(&Site{Name: "acme", Path: dir, ContainerPort: 8080}); ok {
		t.Error("a custom container declaring no workers resolved to a framework with none")
	}
}

func writeCustomWorkers(t *testing.T, dir string, workers map[string]FrameworkWorker) {
	t.Helper()
	if err := SaveProjectConfig(dir, &ProjectConfig{CustomWorkers: workers}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjectConfig(dir); err != nil {
		t.Fatalf("reading back %s: %v", filepath.Join(dir, ".servlo.yaml"), err)
	}
}
