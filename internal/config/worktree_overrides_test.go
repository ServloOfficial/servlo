package config

import (
	"testing"
)

func TestWorktreePHPVersion_inheritsWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	if got := WorktreePHPVersion(dir, "8.3"); got != "8.3" {
		t.Errorf("WorktreePHPVersion() = %q, want %q (inherit)", got, "8.3")
	}
}

func TestWorktreePHPVersion_overridesFromLerdYaml(t *testing.T) {
	dir := t.TempDir()
	if err := SaveProjectConfig(dir, &ProjectConfig{PHPVersion: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if got := WorktreePHPVersion(dir, "8.3"); got != "8.4" {
		t.Errorf("WorktreePHPVersion() = %q, want %q (override)", got, "8.4")
	}
}

func TestWorktreePHPVersion_emptyOverrideInherits(t *testing.T) {
	dir := t.TempDir()
	if err := SaveProjectConfig(dir, &ProjectConfig{NodeVersion: "22"}); err != nil {
		t.Fatal(err)
	}
	if got := WorktreePHPVersion(dir, "8.3"); got != "8.3" {
		t.Errorf("WorktreePHPVersion() with empty PHP override = %q, want %q", got, "8.3")
	}
}

func TestWorktreeNodeVersion_overridesFromLerdYaml(t *testing.T) {
	dir := t.TempDir()
	if err := SaveProjectConfig(dir, &ProjectConfig{NodeVersion: "24"}); err != nil {
		t.Fatal(err)
	}
	if got := WorktreeNodeVersion(dir, "22"); got != "24" {
		t.Errorf("WorktreeNodeVersion() = %q, want %q", got, "24")
	}
}

func TestSetWorktreePHPVersion_createsLerdYamlWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if err := SetWorktreePHPVersion(dir, "8.4"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PHPVersion != "8.4" {
		t.Errorf("after Set, PHPVersion = %q, want %q", cfg.PHPVersion, "8.4")
	}
}

func TestSetWorktreePHPVersion_emptyClearsOverride(t *testing.T) {
	dir := t.TempDir()
	if err := SaveProjectConfig(dir, &ProjectConfig{PHPVersion: "8.4", NodeVersion: "22"}); err != nil {
		t.Fatal(err)
	}
	if err := SetWorktreePHPVersion(dir, ""); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PHPVersion != "" {
		t.Errorf("PHPVersion = %q, want empty after clearing", cfg.PHPVersion)
	}
	if cfg.NodeVersion != "22" {
		t.Errorf("NodeVersion = %q, want %q (untouched)", cfg.NodeVersion, "22")
	}
}

func TestSetWorktreeNodeVersion_createsAndClears(t *testing.T) {
	dir := t.TempDir()
	if err := SetWorktreeNodeVersion(dir, "24"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadProjectConfig(dir)
	if cfg.NodeVersion != "24" {
		t.Fatalf("after Set, NodeVersion = %q", cfg.NodeVersion)
	}
	if err := SetWorktreeNodeVersion(dir, ""); err != nil {
		t.Fatal(err)
	}
	cfg, _ = LoadProjectConfig(dir)
	if cfg.NodeVersion != "" {
		t.Errorf("NodeVersion = %q, want empty after clearing", cfg.NodeVersion)
	}
}

func TestWorktreeDBIsolated_defaultsToFalse(t *testing.T) {
	if got := WorktreeDBIsolated(t.TempDir()); got {
		t.Errorf("WorktreeDBIsolated default = true, want false")
	}
}

func TestSetWorktreeDBIsolated_roundtrip(t *testing.T) {
	dir := t.TempDir()
	if err := SetWorktreeDBIsolated(dir, true); err != nil {
		t.Fatal(err)
	}
	if !WorktreeDBIsolated(dir) {
		t.Errorf("WorktreeDBIsolated after Set(true) = false")
	}
	if err := SetWorktreeDBIsolated(dir, false); err != nil {
		t.Fatal(err)
	}
	if WorktreeDBIsolated(dir) {
		t.Errorf("WorktreeDBIsolated after Set(false) = true")
	}
}
