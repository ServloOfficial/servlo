package siteops

import "github.com/realrashid/servlo/internal/config"

// BackupExcludes are the paths a backup of this site leaves out.
//
// The site's own list when it has one, and its framework's otherwise, on the
// same two-state rule DeployExcludes follows: a site that saved an empty list
// backs up everything, and a site that never saved one keeps following its
// framework's definition, including one updated after the site was created.
//
// What belongs here is only what a deploy puts back. A restore is exactly as
// good as what the archive carried, so anything an application writes and
// nothing rebuilds, uploaded media above all, must not be on this list.
func BackupExcludes(site *config.Site) ([]string, error) {
	if site.BackupExclude != nil {
		return cleanExcludes(*site.BackupExclude), nil
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok {
		return nil, nil
	}
	return cleanExcludes(fw.BackupExcludes()), nil
}

// SetSiteBackupExclude saves a site's own backup exclude list, or clears it
// back to the framework's when list is nil. Nothing to regenerate: this changes
// what the next backup carries, not how the site is served.
func SetSiteBackupExclude(site *config.Site, list *[]string) error {
	updated := *site
	if list == nil {
		updated.BackupExclude = nil
	} else {
		cleaned := cleanExcludes(*list)
		if cleaned == nil {
			// An empty list, not an absent one. The two mean different things.
			cleaned = []string{}
		}
		updated.BackupExclude = &cleaned
	}
	if err := config.AddSite(updated); err != nil {
		return err
	}
	*site = updated
	return nil
}
