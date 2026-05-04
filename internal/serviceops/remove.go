package serviceops

import (
	"fmt"
	"os"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// RemoveOptions controls optional side effects of RemoveService.
type RemoveOptions struct {
	// RemoveData renames the service's data directory to a timestamped
	// `.pre-remove-<ts>` sibling. The data is recoverable by renaming back.
	// On EXDEV the helper falls back to os.RemoveAll.
	RemoveData bool
}

// Internal seams so tests can swap out the real podman calls. The defaults
// route to the real package functions.
var (
	removeStopUnit     = podman.StopUnit
	removeContainerFn  = podman.RemoveContainer
	removeQuadletFn    = podman.RemoveQuadlet
	removeUnitStatusFn = podman.UnitStatus
)

// RemoveService stops, removes, and (optionally) wipes the data of a service.
// It is the single entry point shared by the CLI, MCP, and UI handlers.
//
// Order:
//  1. emit stopping_unit, StopUnit (only if active/activating; abort on error)
//  2. emit removing_container, RemoveContainer
//  3. (if RemoveData) emit removing_data, rename-aside ~/.local/share/lerd/data/<name>
//  4. emit removing_quadlet, RemoveQuadlet, DaemonReload
//  5. emit removing_config, RemoveCustomService (no-op for default presets)
//  6. emit regenerating_consumers, RegenerateFamilyConsumers (if family known)
//  7. emit done
//
// emit may be nil. Default-preset services are accepted: their YAML doesn't
// exist on disk, so RemoveCustomService is a tolerated no-op.
func RemoveService(name string, opts RemoveOptions, emit func(PhaseEvent)) error {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}
	unit := "lerd-" + name

	var family string
	if existing, err := config.LoadCustomService(name); err == nil {
		family = existing.Family
	}

	emit(PhaseEvent{Phase: "stopping_unit", Unit: unit})
	status, _ := removeUnitStatusFn(unit)
	if status == "active" || status == "activating" {
		if err := removeStopUnit(unit); err != nil {
			return fmt.Errorf("stop %s: %w", unit, err)
		}
	}

	emit(PhaseEvent{Phase: "removing_container", Unit: unit})
	removeContainerFn(unit)

	if opts.RemoveData {
		dir := config.DataSubDir(name)
		emit(PhaseEvent{Phase: "removing_data", Message: dir})
		if err := renameDataAside(dir); err != nil {
			return fmt.Errorf("rename data aside for %s: %w", name, err)
		}
	}

	emit(PhaseEvent{Phase: "removing_quadlet", Unit: unit})
	if err := removeQuadletFn(unit); err != nil {
		return fmt.Errorf("remove quadlet %s: %w", unit, err)
	}
	_ = podman.DaemonReloadFn()

	emit(PhaseEvent{Phase: "removing_config"})
	if err := config.RemoveCustomService(name); err != nil {
		return fmt.Errorf("remove service config: %w", err)
	}

	emit(PhaseEvent{Phase: "regenerating_consumers"})
	if family != "" {
		RegenerateFamilyConsumers(family)
	}

	emit(PhaseEvent{Phase: "done"})
	return nil
}

// renameDataAside renames dir to `<dir>.pre-remove-<unix-nanos>` so the data
// is recoverable. Falls back to os.RemoveAll on EXDEV (cross-filesystem). A
// missing source is a no-op.
func renameDataAside(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	aside := fmt.Sprintf("%s.pre-remove-%d", dir, time.Now().UnixNano())
	if err := os.Rename(dir, aside); err != nil {
		if isCrossDeviceErr(err) {
			return os.RemoveAll(dir)
		}
		return err
	}
	return nil
}

func isCrossDeviceErr(err error) bool {
	if err == nil {
		return false
	}
	// LinkError wraps errno EXDEV. Match by string to avoid pulling in syscall.
	return containsEXDEV(err.Error())
}

func containsEXDEV(s string) bool {
	for _, needle := range []string{"invalid cross-device link", "EXDEV", "cross-device"} {
		if len(s) >= len(needle) {
			for i := 0; i+len(needle) <= len(s); i++ {
				if s[i:i+len(needle)] == needle {
					return true
				}
			}
		}
	}
	return false
}
