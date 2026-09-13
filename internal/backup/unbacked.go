package backup

import (
	"github.com/ServloOfficial/servlo/internal/alerts"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/siteops"
)

// A site nothing has ever backed up.
//
// The list says a backup failed and that one could not be restored. It said
// nothing about a site that has never had one, which is the worse state and the
// quieter one: a failure is something that happened, and this is the absence of
// anything happening at all. An operator who added a site meaning to set the
// schedule up afterwards had no way to find out they did not, short of opening
// that site's backup card and reading the word "not scheduled".
//
// Nothing is scheduled on anyone's behalf here. Writing timers for an operator
// is a different decision from telling them there are none, and only the second
// one is servlo's to make.

// ReportUnbacked raises an alert for each site with nothing to restore from and
// nothing that would produce one, takes the alert away from every site that has
// either, and returns the names it raised for so a command run by hand can say
// so on the spot rather than only in the panel.
//
// Both halves have to be missing. An archive is something to restore from
// however it got there, and a schedule means one arrives tonight, so either on
// its own is a choice rather than a gap.
func ReportUnbacked(sites []config.Site, parkedDirs []string) []string {
	var unbacked []string
	for _, site := range sites {
		// A parked domain is a placeholder with nothing in it. Alerting that it
		// has no backups is the clutter that teaches an operator to stop
		// reading the list.
		if siteops.IsParkedSite(site.Path, parkedDirs) {
			continue
		}
		if hasBackupOrWill(site) {
			clearFor(alerts.KindBackupNone, site.Name)
			continue
		}
		unbacked = append(unbacked, site.Name)
		report(alerts.Alert{
			Kind: alerts.KindBackupNone,
			Site: site.Name,
			Message: "No backup of this site has ever been taken, and no schedule would take one. " +
				"Its files and its database exist only on this server, so whatever takes the server " +
				"takes them.\n\n" +
				"Set a schedule on the site's Backups card, and add a destination so the archives " +
				"are not sitting on the machine they are backups of.",
		})
	}
	return unbacked
}

// hasBackupOrWill answers whether there is anything to restore from, or anything
// on its way.
//
// An archive directory that cannot be read is not an answer, and is treated as
// one rather than as "this site has no backups": an alert that cannot tell a
// missing backup from a missing directory is worse than no alert.
func hasBackupOrWill(site config.Site) bool {
	if site.Backup != nil && site.Backup.Schedule != "" && !site.Backup.Disabled {
		return true
	}
	list, err := List(config.SiteBackupsDir(), config.SiteSlug(site.Name))
	if err != nil {
		return true
	}
	return len(list) > 0
}
