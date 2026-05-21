package podman

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

//go:embed spxbridge
var spxBridgeFS embed.FS

// SpxIni returns the SPX conf.d ini with the per-install http key
// substituted. The ini is bind-mounted read-only into every FPM container
// at /usr/local/etc/php/conf.d/zz-lerd-spx.ini. SPX is always loaded and
// its HTTP UI always enabled; per-request profiling only happens when the
// SPX_ENABLED cookie is present, which lerd injects via nginx for armed
// sites.
func SpxIni() (string, error) {
	b, err := spxBridgeFS.ReadFile("spxbridge/zz-lerd-spx.ini")
	if err != nil {
		return "", fmt.Errorf("spx ini embed: %w", err)
	}
	key, err := config.LoadOrGenerateProfilerKey()
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(b), "{{ SPX_KEY }}", key), nil
}

// EnsureProfilerAssets writes the SPX conf.d ini and creates the report data
// directory so the always-mounted FPM volumes have valid bind-mount sources.
// Idempotent; replaces a directory podman may have auto-created at the ini path.
func EnsureProfilerAssets() error {
	if err := os.MkdirAll(config.SpxAssetsDir(), 0755); err != nil {
		return fmt.Errorf("creating spx dir: %w", err)
	}
	if err := os.MkdirAll(config.SpxDataDir(), 0755); err != nil {
		return fmt.Errorf("creating spx data dir: %w", err)
	}

	ini, err := SpxIni()
	if err != nil {
		return err
	}
	path := config.SpxIniFile()
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			if rmErr := os.RemoveAll(path); rmErr != nil {
				return fmt.Errorf("removing stale spx ini directory %s: %w", path, rmErr)
			}
		} else if existing, readErr := os.ReadFile(path); readErr == nil && string(existing) == ini {
			return nil
		}
	}
	if err := os.WriteFile(path, []byte(ini), 0644); err != nil {
		return fmt.Errorf("writing spx ini %s: %w", path, err)
	}
	return nil
}
