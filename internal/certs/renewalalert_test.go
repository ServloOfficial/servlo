package certs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/alerts"
)

// captureAlerts swaps the raiser for one that records, and returns a function
// that reads back what was raised.
//
// It used to wait on a channel with a deadline, and then sleep in case a
// spurious extra alert was still on its way, because the raise happened in a
// goroutine. Raising synchronously makes every alert already there by the time
// the call returns, so a slice answers the same questions without the waiting
// or the flakiness: the deadline version raced its own t.Cleanup restoring the
// seam, which is how this was found.
func captureAlerts(t *testing.T) func(n int) []alerts.Alert {
	t.Helper()
	var raised []alerts.Alert
	old := raiseAlert
	raiseAlert = func(a alerts.Alert) error {
		raised = append(raised, a)
		return nil
	}
	t.Cleanup(func() { raiseAlert = old })

	return func(n int) []alerts.Alert {
		t.Helper()
		if len(raised) < n {
			t.Fatalf("only %d of %d alerts were raised", len(raised), n)
		}
		return raised
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

// A short-lived process must not outrun its own alert.
//
// `servlo secure` on a domain whose DNS is not ready fails, records, and exits.
// The record and the audit entry are written before it returns; the alert was
// raised in a goroutine nobody waits for, so the banner and the email that
// CLAUDE.md section 3.3 calls the loud half were a race against process exit.
func TestFailedRenewal_RaisesTheAlertBeforeItReturns(t *testing.T) {
	renewalEnv(t)

	entered := make(chan alerts.Alert, 1)
	release := make(chan struct{})
	old := raiseAlert
	raiseAlert = func(a alerts.Alert) error {
		entered <- a
		<-release
		return nil
	}
	t.Cleanup(func() {
		close(release)
		raiseAlert = old
	})

	returned := make(chan struct{})
	go func() {
		recordFailure("example.com", errors.New("dns-01 challenge timed out"))
		close(returned)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the alert was never raised")
	}

	select {
	case <-returned:
		t.Fatal("recordFailure returned while its alert was still being raised: a process that exits here reports nothing")
	case <-time.After(100 * time.Millisecond):
	}
}
