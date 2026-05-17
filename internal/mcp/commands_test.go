package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// registerTestSite gives the MCP exec functions a real site to look up via
// config.FindSite. Uses XDG_*_HOME tempdirs so we don't touch the user's
// real registry.
func registerTestSite(t *testing.T, name string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: name, Path: dir, Domains: []string{name + ".test"}}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func resultText(t *testing.T, res any) string {
	t.Helper()
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", res)
	}
	content, ok := m["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatalf("result has no content: %+v", m)
	}
	text, _ := content[0]["text"].(string)
	return text
}

func resultIsError(res any) bool {
	m, ok := res.(map[string]any)
	if !ok {
		return false
	}
	return m["isError"] == true
}

func TestExecCommandAdd_RequiresSiteAndName(t *testing.T) {
	res, _ := execCommandAdd(map[string]any{})
	if !resultIsError(res) || !strings.Contains(resultText(t, res), "site is required") {
		t.Errorf("want site required, got %v", res)
	}
	res, _ = execCommandAdd(map[string]any{"site": "x"})
	if !resultIsError(res) || !strings.Contains(resultText(t, res), "name is required") {
		t.Errorf("want name required, got %v", res)
	}
}

func TestExecCommandAdd_RequiresCommandUnlessDisabled(t *testing.T) {
	registerTestSite(t, "alpha")
	res, _ := execCommandAdd(map[string]any{"site": "alpha", "name": "deploy"})
	if !resultIsError(res) {
		t.Fatalf("want error when command missing, got %v", res)
	}
}

func TestExecCommandAdd_PersistsToYaml(t *testing.T) {
	dir := registerTestSite(t, "alpha")
	res, _ := execCommandAdd(map[string]any{
		"site":        "alpha",
		"name":        "deploy",
		"command":     "./bin/deploy",
		"label":       "Deploy",
		"description": "Push to staging",
		"output":      "text",
		"confirm":     true,
		"icon":        "arrow-up",
	})
	if resultIsError(res) {
		t.Fatalf("add failed: %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "added") {
		t.Errorf("response should say added: %s", resultText(t, res))
	}
	cfg, _ := config.LoadProjectConfig(dir)
	if len(cfg.Commands) != 1 || cfg.Commands[0].Name != "deploy" || cfg.Commands[0].Command != "./bin/deploy" {
		t.Errorf("yaml not updated: %+v", cfg.Commands)
	}
	if !cfg.Commands[0].Confirm || cfg.Commands[0].Icon != "arrow-up" {
		t.Errorf("optional fields lost: %+v", cfg.Commands[0])
	}
}

func TestExecCommandAdd_UpdatesExistingByName(t *testing.T) {
	dir := registerTestSite(t, "alpha")
	_, _ = execCommandAdd(map[string]any{"site": "alpha", "name": "deploy", "command": "v1"})
	res, _ := execCommandAdd(map[string]any{"site": "alpha", "name": "deploy", "command": "v2", "label": "v2"})
	if resultIsError(res) {
		t.Fatalf("update failed: %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "updated") {
		t.Errorf("response should say updated: %s", resultText(t, res))
	}
	cfg, _ := config.LoadProjectConfig(dir)
	if len(cfg.Commands) != 1 || cfg.Commands[0].Command != "v2" {
		t.Errorf("update did not replace: %+v", cfg.Commands)
	}
}

func TestExecCommandAdd_DisabledDoesNotRequireCommand(t *testing.T) {
	dir := registerTestSite(t, "alpha")
	res, _ := execCommandAdd(map[string]any{"site": "alpha", "name": "migrate:fresh", "disabled": true})
	if resultIsError(res) {
		t.Fatalf("disabled add failed: %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "suppresses") {
		t.Errorf("response should mention suppress: %s", resultText(t, res))
	}
	cfg, _ := config.LoadProjectConfig(dir)
	if len(cfg.Commands) != 1 || !cfg.Commands[0].Disabled {
		t.Errorf("disabled flag not persisted: %+v", cfg.Commands)
	}
}

func TestExecCommandRemove_RemovesEntry(t *testing.T) {
	dir := registerTestSite(t, "alpha")
	_, _ = execCommandAdd(map[string]any{"site": "alpha", "name": "deploy", "command": "x"})
	res, _ := execCommandRemove(map[string]any{"site": "alpha", "name": "deploy"})
	if resultIsError(res) {
		t.Fatalf("remove failed: %s", resultText(t, res))
	}
	cfg, _ := config.LoadProjectConfig(dir)
	if len(cfg.Commands) != 0 {
		t.Errorf("entry not removed: %+v", cfg.Commands)
	}
}

func TestExecCommandRemove_ReportsMissing(t *testing.T) {
	registerTestSite(t, "alpha")
	res, _ := execCommandRemove(map[string]any{"site": "alpha", "name": "ghost"})
	if !resultIsError(res) || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("want not-found error, got %v", res)
	}
}

func TestExecCommandAdd_HonoursCheckRule(t *testing.T) {
	dir := registerTestSite(t, "alpha")
	_, _ = execCommandAdd(map[string]any{
		"site":           "alpha",
		"name":           "fixtures:load",
		"command":        "bin/console doctrine:fixtures:load",
		"check_composer": "doctrine/doctrine-fixtures-bundle",
	})
	cfg, _ := config.LoadProjectConfig(dir)
	if cfg.Commands[0].Check == nil || cfg.Commands[0].Check.Composer != "doctrine/doctrine-fixtures-bundle" {
		t.Errorf("check rule not set: %+v", cfg.Commands[0])
	}
}
