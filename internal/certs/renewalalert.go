package certs

import (
	"fmt"
	"log"

	"github.com/realrashid/servlo/internal/alerts"
)

// The loud half of "renewal failure is loud" (S3.5).
//
// The banner and the audit entry only reach somebody who is already looking at
// the panel, and the whole point of the story is the failure that goes
// unnoticed for a month. Raising an alert is what reaches an operator who is
// not looking: it puts the failure on the panel's alert list and, when panel
// SMTP is configured, emails it once.
//
// This deliberately does not send its own mail. The alerts package already
// decides what is one failure and what is two, and two notification paths for
// the same event is how a server ends up either silent or flooding.

// raiseAlert and clearAlert are the seam. Swapped in tests, which have no mail
// server and no business dialling one.
var (
	raiseAlert = alerts.Raise
	clearAlert = alerts.Clear
)

// alertRenewalFailure reports that a domain's issuance is failing.
//
// Called on every attempt, not only the first. A renewal retries on a timer and
// an operator who gets an identical email every hour has a filter rule rather
// than an alert, so the repetition is dropped, but it is dropped in the alerts
// package where every other kind of failure is deduplicated the same way.
func alertRenewalFailure(domain string, cause error) {
	// Raised in the background: this is called from the issuance path, and an
	// unreachable mail server must not turn a failed renewal into a hung one.
	go func() {
		err := raiseAlert(alerts.Alert{
			Kind: alerts.KindCertRenewFailed,
			Site: domain,
			Message: fmt.Sprintf("%s\n\nThe site keeps serving its existing certificate "+
				"until that expires. Open the panel to see the failure and retry.", cause),
		})
		if err != nil {
			log.Printf("[certs] could not report the renewal failure for %s: %v", domain, err)
		}
	}()
}

// alertRenewalRecovered takes the alert away once issuance works again.
func alertRenewalRecovered(domain string) {
	if err := clearAlert(alerts.KindCertRenewFailed, domain); err != nil {
		log.Printf("[certs] could not clear the renewal alert for %s: %v", domain, err)
	}
}
