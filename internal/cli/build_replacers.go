package cli

import (
	"sort"

	"github.com/ServloOfficial/servlo/internal/config"
)

// EligibleBuildReplacers returns every framework worker eligible to provide
// assets for the site: replaces_build:true and a matching Check rule. Unlike
// OptedInBuildReplacers it does NOT require the worker to be in the project's
// .servlo.yaml workers: list, so a chooser can offer asset workers the user has
// not opted into yet.
func EligibleBuildReplacers(site *config.Site, path string) []string {
	return buildReplacers(site, path, nil)
}

// OptedInBuildReplacers returns names of workers opted into via .servlo.yaml
// workers:, declared replaces_build:true in the framework yaml, and able to run
// at the given path.
func OptedInBuildReplacers(site *config.Site, path string) []string {
	proj, _ := config.LoadProjectConfig(site.Path)
	if proj == nil || len(proj.Workers) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(proj.Workers))
	for _, n := range proj.Workers {
		wanted[n] = true
	}
	return buildReplacers(site, path, wanted)
}

// buildReplacers is the shared walk. A nil wanted set means every declared
// replacer qualifies; a non-nil one narrows it to the project's opt-ins.
func buildReplacers(site *config.Site, path string, wanted map[string]bool) []string {
	if site == nil || site.Framework == "" {
		return nil
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok {
		return nil
	}
	var out []string
	for name, w := range fw.Workers {
		if !w.ReplacesBuild {
			continue
		}
		if wanted != nil && !wanted[name] {
			continue
		}
		if w.Check != nil && !config.MatchesRule(path, *w.Check) {
			continue
		}
		if ok, _ := workerSupportedOnPlatform(w); !ok {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
