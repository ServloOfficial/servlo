package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetProjectCommand_AppendsToEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := FrameworkCommand{Name: "deploy", Label: "Deploy", Command: "./bin/deploy", Output: "text"}
	if err := SetProjectCommand(dir, cmd); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, _ := LoadProjectConfig(dir)
	if len(cfg.Commands) != 1 || cfg.Commands[0].Name != "deploy" {
		t.Errorf("commands: %+v", cfg.Commands)
	}
}

func TestSetProjectCommand_UpdatesExistingByName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\ncommands:\n  - name: deploy\n    command: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := FrameworkCommand{Name: "deploy", Label: "Deploy v2", Command: "new", Output: "text"}
	if err := SetProjectCommand(dir, cmd); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, _ := LoadProjectConfig(dir)
	if len(cfg.Commands) != 1 {
		t.Fatalf("want 1 command after update, got %d", len(cfg.Commands))
	}
	if cfg.Commands[0].Command != "new" || cfg.Commands[0].Label != "Deploy v2" {
		t.Errorf("update failed: %+v", cfg.Commands[0])
	}
}

func TestSetProjectCommand_RequiresName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := SetProjectCommand(dir, FrameworkCommand{Command: "echo hi"})
	if err == nil {
		t.Fatal("want validation error, got nil")
	}
	if _, ok := err.(*CommandValidationError); !ok {
		t.Errorf("want CommandValidationError, got %T: %v", err, err)
	}
}

func TestSetProjectCommand_CreatesYamlIfMissing(t *testing.T) {
	dir := t.TempDir()
	// No .lerd.yaml yet — SetProjectCommand should create it.
	cmd := FrameworkCommand{Name: "deploy", Label: "Deploy", Command: "./bin/deploy"}
	if err := SetProjectCommand(dir, cmd); err != nil {
		t.Fatalf("set on missing yaml: %v", err)
	}
	cfg, _ := LoadProjectConfig(dir)
	if len(cfg.Commands) != 1 || cfg.Commands[0].Name != "deploy" {
		t.Errorf("yaml not created with command: %+v", cfg)
	}
}

func TestRemoveProjectCommand_DropsExistingEntry(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\ncommands:\n  - name: a\n    command: x\n  - name: b\n    command: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProjectCommand(dir, "a"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cfg, _ := LoadProjectConfig(dir)
	if len(cfg.Commands) != 1 || cfg.Commands[0].Name != "b" {
		t.Errorf("remove failed: %+v", cfg.Commands)
	}
}

func TestRemoveProjectCommand_ReportsMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := RemoveProjectCommand(dir, "ghost")
	if err == nil {
		t.Fatal("want not-found error, got nil")
	}
	if _, ok := err.(*CommandNotFoundError); !ok {
		t.Errorf("want CommandNotFoundError, got %T: %v", err, err)
	}
}

func TestRemoveProjectCommand_NoYamlReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	err := RemoveProjectCommand(dir, "anything")
	if _, ok := err.(*CommandNotFoundError); !ok {
		t.Errorf("missing yaml should report not found, got %T: %v", err, err)
	}
}
