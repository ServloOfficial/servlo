// Package presetfixtures exposes the add-on service presets as an fs.FS for
// tests. Tests wire it under the config preset seam (via
// config.SetExtraPresetsForTest) so mechanism and functionality tests keep
// resolving add-ons — dependencies, families, dashboards, auto-login file
// mounts — without a network fetch.
//
// It serves the real definitions from the in-repo service store rather than a
// copy of them. A copy is how a test suite ends up green against definitions
// nobody ships: the copy taken at S0.8 had already drifted from the store by
// several fields and an entire credential scheme.
package presetfixtures

import (
	"io/fs"

	"github.com/realrashid/servlo/stores"
)

// FS returns the add-on presets as a flat filesystem of <name>.yaml files.
func FS() fs.FS {
	sub, err := fs.Sub(stores.FS(), string(stores.Services))
	if err != nil {
		panic(err)
	}
	return sub
}
