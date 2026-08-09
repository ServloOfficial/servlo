package deploy

import (
	"log"

	"github.com/realrashid/servlo/internal/alerts"
)

// A failed deploy is loud in the panel while somebody is watching it happen,
// and silent afterwards. The deploys that need an alert are the ones nobody
// watched: a webhook firing on a push, or a deploy started from a browser tab
// that was closed before it finished.
//
// It hangs off recording rather than off the deploy itself, because the record
// is the one place every deploy outcome passes through and a second caller
// added later gets the alert without anybody remembering to wire it.

var (
	raise      = alerts.Raise
	clearAlert = alerts.Clear
)

// alertOutcome moves the site's deploy alert to match what just happened.
func alertOutcome(site string, e Entry) {
	if e.OK {
		if err := clearAlert(alerts.KindDeployFailed, site); err != nil {
			log.Printf("[deploy] could not clear the deploy alert for %s: %v", site, err)
		}
		return
	}
	message := e.Error
	if message == "" {
		message = "The deploy did not finish, and did not say why."
	}
	if e.Subject != "" {
		message = e.Subject + "\n\n" + message
	}
	if err := raise(alerts.Alert{Kind: alerts.KindDeployFailed, Site: site, Message: message}); err != nil {
		log.Printf("[deploy] could not record the deploy alert for %s: %v", site, err)
	}
}
