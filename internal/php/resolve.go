package php

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
)

// VersionForDir resolves the PHP version a directory's commands must run on.
// This is the single answer the CLI, the dashboard and the version-scoped
// tools all use, so a command can never exec into a different PHP than the
// container serving the same directory.
//
// A registered site's version wins, because servlo link clamps to the
// framework's supported range and re-detecting would undo the clamp. Only then
// does the project's own configuration apply.
func VersionForDir(dir string) (string, error) {
	if site, _ := config.FindSiteByPath(SiteRootFor(dir)); site != nil && site.PHPVersion != "" {
		return site.PHPVersion, nil
	}
	version, err := DetectVersion(dir)
	if err == nil {
		return version, nil
	}
	cfg, cfgErr := config.LoadGlobal()
	if cfgErr != nil {
		return "", fmt.Errorf("cannot detect PHP version: %w", err)
	}
	return cfg.PHP.DefaultVersion, nil
}

// SiteRootFor returns the registered site path that contains dir, or dir itself
// if no registered site matches. Commands run from anywhere in a project, so
// this walks up to the project root the site was registered at.
func SiteRootFor(dir string) string {
	reg, err := config.LoadSites()
	if err != nil {
		return dir
	}
	dir = filepath.Clean(dir)
	best := ""
	for _, s := range reg.Sites {
		sitePath := filepath.Clean(s.Path)
		if dir == sitePath || strings.HasPrefix(dir, sitePath+string(filepath.Separator)) {
			// Prefer the longest (most-specific) match.
			if len(sitePath) > len(best) {
				best = sitePath
			}
		}
	}
	if best != "" {
		return best
	}
	return dir
}
