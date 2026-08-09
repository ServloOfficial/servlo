package serviceops

import (
	"sort"
	"strings"

	"github.com/realrashid/servlo/internal/config"
)

// Installing the admin UI that goes with a database.
//
// An operator who installs MySQL wants phpMyAdmin. They wanted it the last time
// too, and the time before, and being asked again is not a choice so much as a
// step between them and the thing they came for. So the engine brings its admin
// UI with it.
//
// Which UI belongs to which engine is not written down here. A preset already
// declares what it administers, in admin_for, which is the same field the
// suggestion cards and the dashboard's "open in" button read. This walks that
// declaration backwards: given a service, which presets say they administer it.
// Adding an admin UI for a new engine stays a YAML change.

// AdminUIsFor names the admin UI presets that administer a service and are not
// installed yet.
//
// A preset matches on the service's own name, the preset it came from, its
// family or its env_role, so MariaDB gets phpMyAdmin through the family it
// declares rather than through a list of every MariaDB version. A UI that is
// already installed is left alone, and so is one whose job another installed UI
// is already doing, because two phpMyAdmins is not twice the panel.
func AdminUIsFor(service string) []string {
	targets := adminTargets(service)
	if len(targets) == 0 {
		return nil
	}
	metas, err := config.ListPresets()
	if err != nil {
		return nil
	}
	var out []string
	for _, meta := range metas {
		if len(meta.AdminFor) == 0 || !administers(meta.AdminFor, targets) {
			continue
		}
		if ServiceInstalled(meta.Name) || adminAlreadyCovered(metas, meta.Name, targets) {
			continue
		}
		out = append(out, meta.Name)
	}
	sort.Strings(out)
	return out
}

// adminTargets is every name a preset's admin_for could reasonably use for this
// service: itself, the preset it was installed from, its family, and the
// service it stands in for.
func adminTargets(service string) map[string]bool {
	targets := map[string]bool{service: true}
	add := func(name string) {
		if name != "" {
			targets[name] = true
		}
	}
	if svc, err := config.LoadCustomService(service); err == nil {
		add(svc.Preset)
		add(config.FamilyOf(svc))
		add(config.EnvRoleOf(svc))
	} else {
		add(config.FamilyOfName(service))
		if p, err := config.LoadPreset(service); err == nil {
			add(p.Family)
			add(p.EnvRole)
		}
	}
	return targets
}

func administers(adminFor []string, targets map[string]bool) bool {
	for _, name := range adminFor {
		if targets[strings.TrimSpace(name)] {
			return true
		}
	}
	return false
}

// adminAlreadyCovered reports whether some other installed preset already
// administers these targets, so installing a second engine of the same family
// does not install a second copy of the same UI.
func adminAlreadyCovered(metas []config.PresetMeta, candidate string, targets map[string]bool) bool {
	for _, meta := range metas {
		if meta.Name == candidate || len(meta.AdminFor) == 0 {
			continue
		}
		if administers(meta.AdminFor, targets) && ServiceInstalled(meta.Name) {
			return true
		}
	}
	return false
}
