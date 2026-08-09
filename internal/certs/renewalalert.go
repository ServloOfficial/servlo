package certs

import (
	"fmt"
	"log"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/mailsend"
)

// The email half of "renewal failure is loud" (S3.5).
//
// The banner and the audit entry only reach somebody who is looking at the
// panel, and the whole point of the story is the failure that goes unnoticed
// for a month. The email is what reaches an operator who is not looking.
//
// It goes out through the panel's own SMTP account (S13.2), not a site's: the
// message is about servlo, addressed to whoever runs it, and a site's provider
// is the wrong sender and often the wrong domain. A panel with no account
// configured sends nothing and says nothing, which is the ordinary state of a
// fresh install rather than a fault.

// alertSender is what actually sends. Swapped in tests, which have no mail
// server and no business dialling one.
var alertSender = mailsend.Send

// alertFirstFailure emails the operator the first time a domain's issuance
// starts failing.
//
// The first time only. A renewal retries on a timer, and an operator who gets
// an identical email every hour for three weeks has a filter rule, not an
// alert. The banner and the audit log carry the repetition; this carries the
// news.
func alertFirstFailure(domain string, cause error) {
	acct, ok, err := config.PanelSMTP()
	if err != nil || !ok || !acct.Configured() {
		return
	}
	to := acct.FromAddress
	subject := "Certificate renewal is failing for " + domain
	body := fmt.Sprintf(
		"Servlo could not issue or renew the certificate for %s.\n\n%s\n\n"+
			"The site is still serving its existing certificate until that expires. "+
			"Open the panel to see the failure and retry.",
		domain, cause)

	// Sent in the background: this is called from the issuance path, and an
	// unreachable mail server must not turn a failed renewal into a hung one.
	go func() {
		if err := alertSender(acct, to, subject, body); err != nil {
			log.Printf("[certs] could not email the renewal failure for %s: %v", domain, err)
		}
	}()
}
