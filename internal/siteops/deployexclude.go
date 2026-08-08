package siteops

import (
	"fmt"
	"path"
	"strings"

	"github.com/realrashid/servlo/internal/config"
)

// DeployExcludes are the paths a deploy of this site must not remove.
//
// The site's own list when it has one, and its framework's otherwise. Two
// separate states rather than one, because clearing the list is a decision an
// operator can make and it has to survive: a site that saved an empty list
// protects nothing, and a site that never saved one keeps following whatever
// its framework's definition says, including a definition updated after the
// site was created.
func DeployExcludes(site *config.Site) ([]string, error) {
	if site.DeployExclude != nil {
		return cleanExcludes(*site.DeployExclude), nil
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok {
		return nil, nil
	}
	return cleanExcludes(fw.DeployExcludes()), nil
}

// SetSiteDeployExclude saves a site's own exclude list, or clears it back to
// the framework's when list is nil.
//
// Nothing to regenerate and nothing to reload: this changes what the next
// deploy does, not how the site is served.
func SetSiteDeployExclude(site *config.Site, list *[]string) error {
	updated := *site
	if list == nil {
		updated.DeployExclude = nil
	} else {
		cleaned := cleanExcludes(*list)
		if cleaned == nil {
			// An empty list, not an absent one. The two mean different things
			// and cleaning must not turn one into the other.
			cleaned = []string{}
		}
		updated.DeployExclude = &cleaned
	}
	if err := updated.ValidateDeployExclude(); err != nil {
		return err
	}
	if err := config.AddSite(updated); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}
	*site = updated
	return nil
}

// cleanExcludes drops the entries a list picks up from being typed into a box,
// and normalises the rest so "wp-content/uploads/" and "./wp-content/uploads"
// are the one path they plainly mean.
func cleanExcludes(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range in {
		p = path.Clean(strings.TrimSpace(strings.ReplaceAll(p, `\`, "/")))
		if p == "" || p == "." || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
