package podman

import (
	"fmt"
	"strings"

	"github.com/realrashid/servlo/internal/config"
)

// CustomFPMContainerName returns the per-site PHP-FPM container name for a site
// that serves PHP via fastcgi from its own custom-built image (a PHP project
// with a Containerfile and no port), e.g. "servlo-cfpm-myapp".
func CustomFPMContainerName(siteName string) string {
	return "servlo-cfpm-" + siteName
}

// SharedFPMContainerName returns the shared per-version FPM container name,
// e.g. "servlo-php84-fpm".
func SharedFPMContainerName(version string) string {
	return "servlo-php" + strings.ReplaceAll(version, ".", "") + "-fpm"
}

// FPMContainerName resolves the FPM container nginx fastcgi's to and the php
// shims exec into: a per-site container for custom-FPM sites, otherwise the
// shared servlo-php<version>-fpm container.
func FPMContainerName(site config.Site, version string) string {
	if site.IsCustomFPM() {
		return CustomFPMContainerName(site.Name)
	}
	return SharedFPMContainerName(version)
}

// WriteCustomFPMQuadlet writes a per-site PHP-FPM quadlet running the site's
// custom-built image (CustomImageName) under a per-site container name. It
// reuses the shared FPM template so the container inherits every servlo mount
// (dumps, devtools, the bun volume), overriding only the
// Image and ContainerName. Ensures the shared per-version ini/assets exist
// first, like WriteFPMQuadlet.
func WriteCustomFPMQuadlet(siteName, version string) error {
	if err := EnsureUserIni(version); err != nil {
		return fmt.Errorf("creating user ini: %w", err)
	}
	if err := EnsureSharedIni(); err != nil {
		return fmt.Errorf("creating shared ini: %w", err)
	}
	if err := ensureFPMHostsFile(); err != nil {
		return err
	}

	content, err := generateCustomFPMQuadlet(siteName, version)
	if err != nil {
		return err
	}
	if _, err := WriteQuadletDiff(CustomFPMContainerName(siteName), content); err != nil {
		return err
	}
	return DaemonReloadFn()
}

// generateCustomFPMQuadlet renders the per-site FPM quadlet content: the shared
// FPM template with Image and ContainerName overridden for the site's custom
// image. Pure (no IO), mirroring GenerateFrankenPHPQuadlet.
func generateCustomFPMQuadlet(siteName, version string) (string, error) {
	content, err := renderFPMQuadletContent(version)
	if err != nil {
		return "", err
	}
	short := strings.ReplaceAll(version, ".", "")
	content = strings.ReplaceAll(content, "Image=servlo-php"+short+"-fpm:local", "Image="+CustomImageName(siteName))
	content = strings.ReplaceAll(content, "ContainerName=servlo-php"+short+"-fpm", "ContainerName="+CustomFPMContainerName(siteName))
	// Its own pools, for the same reason every container has its own: a master
	// binds the socket of every pool it can see, so sharing the shared
	// container's directory would have the two fighting over each other's sites.
	content = strings.ReplaceAll(content,
		"Volume="+config.FPMPoolDir(SharedFPMContainerName(version))+":",
		"Volume="+config.FPMPoolDir(CustomFPMContainerName(siteName))+":")
	content = strings.ReplaceAll(content, "Description=Servlo PHP "+version+" FPM", "Description=Servlo PHP "+version+" FPM (custom: "+siteName+")")
	return content, nil
}

// RemoveCustomFPMQuadlet removes the per-site custom FPM quadlet unit file.
func RemoveCustomFPMQuadlet(siteName string) error {
	return RemoveQuadlet(CustomFPMContainerName(siteName))
}

// CustomFPMBaseVersion returns the dotted PHP version a custom-FPM site's
// Containerfile builds FROM (e.g. "FROM servlo-php84-fpm:local" -> "8.4"), or "" when
// the base isn't a servlo FPM image. A custom-FPM site's PHP version is fixed by that
// FROM line, not project detection, so the caller can report the right version and
// mount the matching per-version inis instead of a detected one that may differ.
func CustomFPMBaseVersion(projectPath string, cfg *config.ContainerConfig) string {
	base := ContainerBaseImage(projectPath, cfg)
	if i := strings.IndexByte(base, ':'); i >= 0 {
		base = base[:i]
	}
	if !strings.HasPrefix(base, "servlo-php") || !strings.HasSuffix(base, "-fpm") {
		return ""
	}
	short := strings.TrimSuffix(strings.TrimPrefix(base, "servlo-php"), "-fpm")
	for _, v := range config.SupportedPHPVersions {
		if strings.ReplaceAll(v, ".", "") == short {
			return v
		}
	}
	return ""
}
