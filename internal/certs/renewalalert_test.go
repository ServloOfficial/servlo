package certs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/alerts"
)

// captureAlerts swaps the raiser for one that records, and returns a function
// that waits for n of them. The real one runs in a goroutine so a dead mail
// server cannot hang an issuance.
func captureAlerts(t *testing.T) func(n int) []alerts.Alert {
	t.Helper()
	ch := make(chan alerts.Alert, 8)
	old := raiseAlert
	raiseAlert = func(a alerts.Alert) error {
		ch <- a
		return nil
	}
	t.Cleanup(func() { raiseAlert = old })

	return func(n int) []alerts.Alert {
		t.Helper()
		var got []alerts.Alert
		deadline := time.After(2 * time.Second)
		for len(got) < n {
			select {
			case a := <-ch:
				got = append(got, a)
			case <-deadline:
				t.Fatalf("only %d of %d alerts were raised", len(got), n)
			}
		}
		// Give a spurious extra one a moment to show up.
		select {
		case a := <-ch:
			got = append(got, a)
		case <-time.After(200 * time.Millisecond):
		}
		return got
	}
}

// The banner and the audit entry only reach somebody already looking at the
// panel. The alert is the half that reaches an operator who is not.
func TestFailedRenewal_RaisesAnAlertNamingTheDomainAndTheReason(t *testing.T) {
	renewalEnv(t)
	wait := captureAlerts(t)

	recordFailure("example.com", errors.New("dns-01 challenge timed out"))

	got := wait(1)
	if len(got) != 1 {
		t.Fatalf("raised %d alerts, want exactly 1", len(got))
	}
	if got[0].Kind != alerts.KindCertRenewFailed {
		t.Errorf("raised a %q alert, want %q", got[0].Kind, alerts.KindCertRenewFailed)
	}
	if got[0].Site != "example.com" {
		t.Errorf("the alert is against %q, not the failing domain", got[0].Site)
	}
	if !strings.Contains(got[0].Message, "dns-01 challenge timed out") {
		t.Errorf("the alert does not carry the reason:\n%s", got[0].Message)
	}
	// The subject an operator actually reads has to name the domain, or twenty
	// sites produce twenty indistinguishable emails.
	if !strings.Contains(got[0].Subject(), "example.com") {
		t.Errorf("subject %q does not name the domain", got[0].Subject())
	}
}

// Issuance that works again takes the alert away. An alarm that outlives its
// problem is an alarm operators learn to ignore.
func TestSuccessfulRenewal_ClearsTheAlert(t *testing.T) {
	renewalEnv(t)
	captureAlerts(t)

	var cleared []string
	old := clearAlert
	clearAlert = func(kind, site string) error {
		cleared = append(cleared, kind+"/"+site)
		return nil
	}
	t.Cleanup(func() { clearAlert = old })

	recordFailure("example.com", errors.New("boom"))
	clearFailure("example.com")

	want := alerts.KindCertRenewFailed + "/example.com"
	if len(cleared) != 1 || cleared[0] != want {
		t.Errorf("cleared %v, want [%s]", cleared, want)
	}
}

// A domain that was never failing does not clear an alert it never raised,
// because a successful renewal happens far more often than a failing one and
// rewriting the alert store on each of them is pure churn.
func TestSuccessfulRenewal_ClearsNothingWhenNothingWasFailing(t *testing.T) {
	renewalEnv(t)

	called := false
	old := clearAlert
	clearAlert = func(string, string) error { called = true; return nil }
	t.Cleanup(func() { clearAlert = old })

	clearFailure("example.com")

	if called {
		t.Error("a domain that was never failing tried to clear an alert")
	}
}
