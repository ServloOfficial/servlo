package certs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/realrashid/servlo/internal/auditlog"
	"github.com/realrashid/servlo/internal/config"
)

// Renewal failure, and why it gets its own file.
//
// The dangerous shape is not a certificate that expires. It is a renewal that
// starts failing while the certificate still has a month on it: everything
// keeps working, nobody is told, and thirty days later the site goes down for a
// reason that stopped being visible a month earlier.
//
// So a failure is recorded the moment it happens, kept until an issuance
// actually succeeds, and surfaced in the panel and in doctor. It survives a
// restart because the process that failed the renewal is often the one that
// gets restarted.

// RenewalFailure is one domain whose issuance is failing.
type RenewalFailure struct {
	Domain string `json:"domain"`
	Reason string `json:"reason"`
	// Since is when it first failed, not when it last failed. A renewal that
	// has been failing for three weeks is a different problem from one that
	// failed once this morning, and resetting the clock on every attempt hides
	// which one an operator is looking at.
	Since time.Time `json:"since"`
	// LastAttempt is when it most recently tried.
	LastAttempt time.Time `json:"last_attempt"`
}

var failureMu sync.Mutex

func failuresPath() string { return filepath.Join(config.CertsDir(), "renewal-failures.json") }

func readFailures() map[string]RenewalFailure {
	out := map[string]RenewalFailure{}
	data, err := os.ReadFile(failuresPath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

func writeFailures(m map[string]RenewalFailure) {
	if err := os.MkdirAll(filepath.Dir(failuresPath()), 0755); err != nil {
		return
	}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = os.WriteFile(failuresPath(), data, 0644)
}

// recordFailure notes that issuance for a domain failed, and writes the audit
// entry. The first failure sets Since; later ones only move LastAttempt.
func recordFailure(domain string, cause error) {
	failureMu.Lock()
	defer failureMu.Unlock()

	now := time.Now().UTC()
	failures := readFailures()
	f, existing := failures[domain]
	if !existing {
		f = RenewalFailure{Domain: domain, Since: now}
	}
	// Raised on every attempt; the alerts package is where a repeat of the same
	// failure stops being news. See renewalalert.go.
	alertRenewalFailure(domain, cause)
	f.Reason = cause.Error()
	f.LastAttempt = now
	failures[domain] = f
	writeFailures(failures)

	// Every attempt is audited, not just the first: the audit log is the record
	// of what happened, and "it has failed hourly since Tuesday" is exactly the
	// shape a diagnosis needs.
	auditlog.Record(auditlog.Entry{
		At:      now,
		Action:  "cert.issue.failed",
		Subject: domain,
		Detail:  cause.Error(),
	})
}

// clearFailure forgets a domain's failure after a successful issuance. An
// alarm that outlives its problem is an alarm operators learn to ignore.
func clearFailure(domain string) {
	failureMu.Lock()
	defer failureMu.Unlock()

	failures := readFailures()
	if _, ok := failures[domain]; !ok {
		return
	}
	delete(failures, domain)
	writeFailures(failures)
	alertRenewalRecovered(domain)
	auditlog.Record(auditlog.Entry{Action: "cert.issue.recovered", Subject: domain})
}

// RenewalFailures returns every domain whose issuance is currently failing, for
// the panel banner and for doctor.
func RenewalFailures() []RenewalFailure {
	failureMu.Lock()
	defer failureMu.Unlock()

	failures := readFailures()
	out := make([]RenewalFailure, 0, len(failures))
	for _, f := range failures {
		out = append(out, f)
	}
	return out
}

// ExpiryProblem is a certificate on disk that is expired, missing, or close
// enough to expiry to be worth saying out loud.
type ExpiryProblem struct {
	Domain string `json:"domain"`
	// Expired covers both an expired certificate and a missing one: nginx
	// cannot serve the site in either case, so they are equally urgent.
	Expired bool   `json:"expired"`
	Reason  string `json:"reason"`
	// NotAfter is zero for a missing or unreadable certificate.
	NotAfter time.Time `json:"not_after,omitempty"`
}

// ExpiryProblems inspects the certificates for the given domains.
//
// Separate from RenewalFailures on purpose. A failure record says renewal is
// not working; this says what is actually on disk right now. A machine that was
// restored from a backup, or whose panel has never run, has no failure records
// and can still be serving something expired, and that must not be quiet.
func ExpiryProblems(domains []string) []ExpiryProblem {
	certsDir := filepath.Join(config.CertsDir(), "sites")
	var out []ExpiryProblem
	for _, domain := range domains {
		path := filepath.Join(certsDir, domain+".crt")
		leaf, err := readLeaf(path)
		if err != nil {
			out = append(out, ExpiryProblem{
				Domain: domain, Expired: true,
				Reason: fmt.Sprintf("no usable certificate on disk (%v)", err),
			})
			continue
		}
		switch remaining := time.Until(leaf.NotAfter); {
		case remaining <= 0:
			out = append(out, ExpiryProblem{
				Domain: domain, Expired: true, NotAfter: leaf.NotAfter,
				Reason: fmt.Sprintf("the certificate expired on %s", leaf.NotAfter.Format(time.DateOnly)),
			})
		case remaining < certReissueWindow:
			out = append(out, ExpiryProblem{
				Domain: domain, NotAfter: leaf.NotAfter,
				Reason: fmt.Sprintf("the certificate expires on %s, in %d days", leaf.NotAfter.Format(time.DateOnly), int(remaining.Hours()/24)),
			})
		}
	}
	return out
}
