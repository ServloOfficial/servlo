package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// The PHP settings production mode changes, and why these four.
//
// display_errors decides whether a visitor sees a stack trace or a blank page.
// A stack trace names file paths, database hosts, and often enough of a query
// to be worth an attacker's time, so on a public site it is the single most
// valuable thing to turn off.
//
// expose_php removes the X-Powered-By header. Small, and worth it: it hands a
// scanner the exact PHP version to look up known vulnerabilities against, for
// no benefit to anybody.
//
// OPcache with validate_timestamps=0 stops PHP stat-ing every file on every
// request. That is a real speed-up on a busy site and the reason a deploy has
// to reload FPM: without a reload, PHP keeps serving the code it cached. That
// tradeoff is right in production and wrong while someone is editing files,
// which is exactly why it is behind the mode flag.

// productionIniName sorts before the shared (95) and per-version (98) files, so
// anything an operator sets by hand still wins. conf.d loads alphabetically and
// the last value read is the one that applies.
const productionIniName = "90-production.ini"

// ProductionIniFile is the host path of the servlo-managed production drop-in.
//
// It lives here rather than in internal/phpini because podman needs the path to
// mount it and phpini imports php, which imports podman.
func ProductionIniFile() string {
	return filepath.Join(DataDir(), "php", "shared", productionIniName)
}

// productionBody is what the drop-in contains when the mode is on.
const productionBody = `; Written by servlo when production mode is on.
; Do not edit: it is rewritten on every start. Override in 95-shared.ini or the
; per-version 98-user.ini, both of which load after this one and win.
display_errors = Off
display_startup_errors = Off
expose_php = Off
opcache.enable = 1
opcache.enable_cli = 0
opcache.validate_timestamps = 0
`

// developmentBody is what it contains when the mode is off.
//
// Written rather than deleted, so turning production mode off actively restores
// the development values instead of leaving whatever the image happened to
// ship. A file that disappears leaves the previous behaviour in place on a
// container that has not been rebuilt.
const developmentBody = `; Written by servlo when production mode is off.
; Do not edit: it is rewritten on every start. Override in 95-shared.ini or the
; per-version 98-user.ini, both of which load after this one and win.
display_errors = On
display_startup_errors = On
expose_php = On
opcache.enable = 1
opcache.enable_cli = 0
opcache.validate_timestamps = 1
opcache.revalidate_freq = 0
`

// Body returns the drop-in contents for a mode.
func Body(production bool) string {
	if production {
		return productionBody
	}
	return developmentBody
}

// WriteProductionIni writes the drop-in for the current mode. Called on start
// and whenever the mode changes, so the file on disk always matches the flag
// rather than whatever it was when someone last remembered to regenerate it.
func WriteProductionIni(production bool) error {
	path := ProductionIniFile()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("preparing the php config directory: %w", err)
	}
	return os.WriteFile(path, []byte(Body(production)), 0644)
}
