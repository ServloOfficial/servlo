package serviceops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// TestEnsureDefaultPresetQuadletPinned_preservesImage covers the regression
// the v1.19.0-beta.6 fix targets — a `lerd update` install rewrite must not
// silently jump default-preset users from their installed minor to whatever
// preset.Image declares. Reinstall now preserves the on-disk image even
// after RemoveService deletes the quadlet by passing the captured image to
// EnsureDefaultPresetQuadletPinned.
func TestEnsureDefaultPresetQuadletPinned_preservesImage(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }

	pinned := "docker.io/getmeili/meilisearch:v1.7.0"
	if err := EnsureDefaultPresetQuadletPinned("meilisearch", pinned); err != nil {
		t.Fatalf("EnsureDefaultPresetQuadletPinned: %v", err)
	}

	quadletPath := filepath.Join(config.QuadletDir(), "lerd-meilisearch.container")
	data, err := os.ReadFile(quadletPath)
	if err != nil {
		t.Fatalf("read quadlet: %v", err)
	}
	if !strings.Contains(string(data), "Image="+pinned) {
		t.Errorf("expected Image=%s in quadlet, got:\n%s", pinned, string(data))
	}
}

// TestEnsureDefaultPresetQuadletPinned_emptyPinFallsThroughToPreset verifies
// that passing pinnedImage="" preserves existing behaviour: the function
// still resolves the image via the preset/strategy/track_latest path.
func TestEnsureDefaultPresetQuadletPinned_emptyPinFallsThroughToPreset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }

	if err := EnsureDefaultPresetQuadletPinned("meilisearch", ""); err != nil {
		t.Fatalf("EnsureDefaultPresetQuadletPinned with empty pin: %v", err)
	}
	quadletPath := filepath.Join(config.QuadletDir(), "lerd-meilisearch.container")
	data, err := os.ReadFile(quadletPath)
	if err != nil {
		t.Fatalf("read quadlet: %v", err)
	}
	if !strings.Contains(string(data), "Image=docker.io/getmeili/meilisearch:") {
		t.Errorf("expected meilisearch Image= line in quadlet, got:\n%s", string(data))
	}
}

func TestCaptureReinstallSpec_emptyPresetVersionRejected(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// mysql is multi-version, so a custom-service YAML with empty
	// preset_version is a corruption that would silently bump the user
	// off whatever tag they were running. captureReinstallSpec must
	// refuse rather than fall through to DefaultVersion.
	svc := &config.CustomService{
		Name:          "mysql",
		Image:         "docker.io/library/mysql:8.4.9",
		Family:        "mysql",
		PresetVersion: "",
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	_, err := captureReinstallSpec("mysql")
	if err == nil {
		t.Fatal("expected error refusing reinstall with empty preset_version")
	}
	if !strings.Contains(err.Error(), "preset_version") {
		t.Errorf("error should mention preset_version: %v", err)
	}
}

func TestCaptureReinstallSpec_capturesImageForDefaultPreset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	orig := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = orig })
	podman.DaemonReloadFn = func() error { return nil }

	// Plant a default-preset quadlet whose Image= differs from the preset's
	// canonical tag. captureReinstallSpec must read this off disk so the
	// reinstall preserves it across the RemoveService → install hop.
	if err := os.MkdirAll(config.QuadletDir(), 0o755); err != nil {
		t.Fatalf("mkdir QuadletDir: %v", err)
	}
	planted := "[Container]\nImage=docker.io/getmeili/meilisearch:v1.7.0\nContainerName=lerd-meilisearch\n"
	if err := os.WriteFile(filepath.Join(config.QuadletDir(), "lerd-meilisearch.container"), []byte(planted), 0o644); err != nil {
		t.Fatalf("write planted quadlet: %v", err)
	}

	spec, err := captureReinstallSpec("meilisearch")
	if err != nil {
		t.Fatalf("captureReinstallSpec: %v", err)
	}
	if spec.image != "docker.io/getmeili/meilisearch:v1.7.0" {
		t.Errorf("expected captured image v1.7.0, got %q", spec.image)
	}
}
