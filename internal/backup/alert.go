package backup

import (
	"fmt"
	"log"
	"strings"

	"github.com/realrashid/servlo/internal/alerts"
)

// Alerting a backup, and why it is here rather than in the caller.
//
// A backup runs on a timer, in the middle of the night, with nobody watching.
// The command prints its failure to a journal an operator reads once a quarter,
// which means a scheduled backup that has been failing since March is the
// normal way a server ends up with no backups at all. So the outcome is
// reported to the alert list every time, by whichever caller ran it, and the
// deciding is done in one place so the CLI and the panel cannot disagree about
// what counts as a failure.

var (
	raise      = alerts.Raise
	clearAlert = alerts.Clear
)

// Report records what a backup run came to.
//
// A destination that could not be reached counts. The archive is on this
// server and usable, which is why the command reports it as a warning rather
// than a failure, but an archive that only exists on the machine it is a backup
// of is not a backup, and nobody finds out on the day it matters.
func Report(site string, rec Record, runErr error) {
	switch {
	case runErr != nil:
		report(alerts.Alert{
			Kind:    alerts.KindBackupFailed,
			Site:    site,
			Message: "The backup did not complete.\n\n" + runErr.Error(),
		})
	case len(rec.SendErrors) > 0:
		var why []string
		for _, err := range rec.SendErrors {
			why = append(why, err.Error())
		}
		report(alerts.Alert{
			Kind: alerts.KindBackupFailed,
			Site: site,
			Message: "The archive was written on this server but could not be copied off it, " +
				"so there is no offsite copy of this backup.\n\n" + strings.Join(why, "\n\n"),
		})
	default:
		clearFor(alerts.KindBackupFailed, site)
	}
}

// ReportVerify records whether the newest archive could actually be restored,
// which is the only question a backup has ever really been asked.
func ReportVerify(site string, archive string, err error) {
	if err != nil {
		report(alerts.Alert{
			Kind: alerts.KindBackupUnverified,
			Site: site,
			Message: fmt.Sprintf("%s could not be restored into a scratch database, so it is "+
				"not known to be a working backup.\n\n%v", archive, err),
		})
		return
	}
	clearFor(alerts.KindBackupUnverified, site)
}

func report(a alerts.Alert) {
	if err := raise(a); err != nil {
		log.Printf("[backup] could not record the %s alert: %v", a.Kind, err)
	}
}

func clearFor(kind, site string) {
	if err := clearAlert(kind, site); err != nil {
		log.Printf("[backup] could not clear the %s alert: %v", kind, err)
	}
}
