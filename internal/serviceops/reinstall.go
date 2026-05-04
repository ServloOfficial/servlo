package serviceops

import (
	"fmt"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// reinstallSpec captures every piece of state we need to reproduce after
// RemoveService runs. Built before RemoveService so the fresh install can
// pin the same image / version the user was running, instead of re-resolving
// the rolling preset.Image and silently jumping versions.
type reinstallSpec struct {
	// version is the multi-version preset tag (e.g. "8.4" for mysql). Empty
	// for single-version and default presets.
	version string
	// image is the fully-qualified Image= line from the on-disk quadlet
	// (e.g. "docker.io/library/mysql:8.4.9"). Empty when there's no quadlet.
	image string
}

// Seams for ReinstallService composition. The defaults wire to the real
// install + reprovision functions; tests substitute fakes.
var (
	reinstallInstallFn = realReinstallInstall
	reinstallReprovFn  = ReprovisionLinkedSites
)

// ReinstallService stops, removes, and reinstalls a service, optionally wiping
// its data and reprovisioning per-site state (databases, buckets) on the
// fresh service.
//
// Reinstall requires that the service is currently installed: for default
// presets that means a quadlet on disk; for custom services that means a
// custom-service YAML. If neither is found ReinstallService returns an error.
//
// When resetData is true the data dir is renamed-aside (recoverable as
// .pre-remove-<ts>) and ReprovisionLinkedSites is invoked after the install
// completes so dependent sites' DBs/buckets exist on the fresh service.
func ReinstallService(name string, resetData bool, emit func(PhaseEvent)) error {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}
	emit(PhaseEvent{Phase: "reinstall_starting"})

	spec, err := captureReinstallSpec(name)
	if err != nil {
		return err
	}

	// Pre-flight: if the image is already pulled locally we can guarantee the
	// install step won't fail on a missing image after RemoveService has
	// already deleted the quadlet (no rollback on a failed pull). For images
	// not yet local, the install step will pull and surface failures normally.
	if spec.image != "" && podman.ImageExists(spec.image) {
		emit(PhaseEvent{Phase: "preflight_image_ok", Image: spec.image})
	}

	if err := RemoveService(name, RemoveOptions{RemoveData: resetData}, emit); err != nil {
		return fmt.Errorf("reinstall: remove step: %w", err)
	}

	if _, err := reinstallInstallFn(name, spec, emit); err != nil {
		return fmt.Errorf("reinstall: install step: %w", err)
	}

	if resetData {
		if err := reinstallReprovFn(name, emit); err != nil {
			return fmt.Errorf("reinstall: reprovision step: %w", err)
		}
	}
	return nil
}

// captureReinstallSpec snapshots the on-disk state RemoveService is about to
// destroy, so realReinstallInstall can pin the same version + image. For
// custom services the version comes from the YAML; for default presets we
// only have the rendered quadlet's Image line.
func captureReinstallSpec(name string) (reinstallSpec, error) {
	if existing, err := config.LoadCustomService(name); err == nil {
		// Multi-version presets must always carry a PresetVersion. An empty
		// value here is a YAML corruption — falling through to InstallPresetByName
		// with version="" would silently resolve to DefaultVersion and move
		// the user off whatever tag they were running.
		if preset, perr := config.LoadPreset(name); perr == nil && len(preset.Versions) > 0 && existing.PresetVersion == "" {
			return reinstallSpec{}, fmt.Errorf("service %q has empty preset_version; refusing to reinstall and silently jump to default version", name)
		}
		return reinstallSpec{version: existing.PresetVersion, image: existing.Image}, nil
	}
	if config.IsDefaultPreset(name) && podman.QuadletInstalled("lerd-"+name) {
		return reinstallSpec{image: podman.InstalledImage("lerd-" + name)}, nil
	}
	return reinstallSpec{}, fmt.Errorf("service %q is not installed; nothing to reinstall", name)
}

// realReinstallInstall dispatches to the right install path based on whether
// name refers to a default preset or a custom service. For default presets we
// inject the captured image so EnsureDefaultPresetQuadlet pins the same tag
// the user was running, sidestepping the rolling preset.Image bump that
// EnsureDefaultPresetQuadlet would otherwise apply now that RemoveService has
// deleted the on-disk quadlet (InstalledImage returns "" mid-reinstall).
func realReinstallInstall(name string, spec reinstallSpec, emit func(PhaseEvent)) (*config.CustomService, error) {
	if config.IsDefaultPreset(name) {
		emit(PhaseEvent{Phase: "installing_config"})
		if err := EnsureDefaultPresetQuadletPinned(name, spec.image); err != nil {
			return nil, err
		}
		_ = podman.DaemonReloadFn()

		// Pull the pinned image up-front so a missing/unreachable registry
		// surfaces here as a clear error instead of a 60s WaitReady timeout.
		if spec.image != "" && !podman.ImageExists(spec.image) {
			emit(PhaseEvent{Phase: "pulling_image", Image: spec.image})
			if err := podman.PullImageWithProgress(spec.image, func(line string) {
				emit(PhaseEvent{Phase: "pulling_image", Message: line})
			}); err != nil {
				return nil, fmt.Errorf("pulling %s: %w", spec.image, err)
			}
		}

		unit := "lerd-" + name
		emit(PhaseEvent{Phase: "starting_unit", Unit: unit})
		var startErr error
		for attempt := range 5 {
			startErr = podman.StartUnit(unit)
			if startErr == nil || !strings.Contains(startErr.Error(), "not found") {
				break
			}
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
		}
		if startErr != nil {
			return nil, startErr
		}
		_ = config.SetServicePaused(name, false)
		_ = config.SetServiceManuallyStarted(name, true)

		emit(PhaseEvent{Phase: "waiting_ready", Unit: unit})
		if err := podman.WaitReady(name, 60*time.Second); err != nil {
			return nil, err
		}
		return &config.CustomService{Name: name}, nil
	}
	return InstallPresetStreaming(name, spec.version, emit)
}
