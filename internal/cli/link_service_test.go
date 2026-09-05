package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/feedback"
)

// SQLite is a per-project file, not a container. linkApplyServices must skip it
// rather than route it through ensureServiceRunning, which looks for a preset or
// custom service YAML and warns "custom service sqlite not found" when neither
// exists (there is no sqlite preset by design).
func TestLinkApplyServices_SkipsSQLite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	var buf bytes.Buffer
	defer feedback.SetTestWriter(&buf)()

	proj := &config.ProjectConfig{Services: []config.ProjectService{{Name: "sqlite"}}}
	if err := linkApplyServices(t.TempDir(), proj); err != nil {
		t.Fatalf("linkApplyServices: %v", err)
	}
	if strings.Contains(buf.String(), "sqlite") {
		t.Errorf("sqlite should be skipped silently, got output: %q", buf.String())
	}
}

// An inline service definition names an image and a command chosen by the
// repository, which may have been cloned from anywhere, so servlo will not run
// it. The site still links: the entry is dropped and the operator is told which
// service went and how to get it back.
func TestLinkApplyServices_RefusesAnInlineDefinition(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	var buf bytes.Buffer
	defer feedback.SetTestWriter(&buf)()

	dir := t.TempDir()
	yaml := "services:\n" +
		"  - mongodb:\n" +
		"      image: docker.io/library/mongo:7\n" +
		"      ports:\n" +
		"        - 27017:27017\n"
	if err := os.WriteFile(filepath.Join(dir, ".servlo.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	proj, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	if err := linkApplyServices(dir, proj); err != nil {
		t.Fatalf("linkApplyServices: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "mongodb") {
		t.Errorf("the refusal must name the service it dropped, got: %q", out)
	}
	if !strings.Contains(out, "servlo service") {
		t.Errorf("the refusal must say how to install the service instead, got: %q", out)
	}
	// Nothing may have been written to the machine's custom-service store.
	if _, err := config.LoadCustomService("mongodb"); err == nil {
		t.Error("an inline definition must not be registered as a custom service")
	}
}
