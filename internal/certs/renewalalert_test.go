package certs

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

type sentMail struct {
	acct             config.SMTPSettings
	to, subject, msg string
}

// captureAlerts swaps the sender for one that records, and returns a function
// that waits for n messages. The real one runs in a goroutine so a dead mail
// server cannot hang an issuance.
func captureAlerts(t *testing.T) func(n int) []sentMail {
	t.Helper()
	ch := make(chan sentMail, 8)
	old := alertSender
	alertSender = func(acct config.SMTPSettings, to, subject, body string) error {
		ch <- sentMail{acct, to, subject, body}
		return nil
	}
	t.Cleanup(func() { alertSender = old })

	return func(n int) []sentMail {
		t.Helper()
		var got []sentMail
		deadline := time.After(2 * time.Second)
		for len(got) < n {
			select {
			case m := <-ch:
				got = append(got, m)
			case <-deadline:
				t.Fatalf("only %d of %d alerts were sent", len(got), n)
			}
		}
		// Give a spurious extra one a moment to show up.
		select {
		case m := <-ch:
			got = append(got, m)
		case <-time.After(200 * time.Millisecond):
		}
		return got
	}
}

func panelAccount(t *testing.T) {
	t.Helper()
	if err := config.SavePanelSMTP(config.SMTPSettings{
		Host: "smtp.panel.example", Port: 587, Username: "ops", Password: "s3cret",
		Encryption: config.SMTPStartTLS, FromAddress: "alerts@panel.example", FromName: "Servlo",
	}); err != nil {
		t.Fatal(err)
	}
}

// The banner and the audit entry only reach somebody already looking at the
// panel. The email is the half that reaches an operator who is not.
func TestFailedRenewal_EmailsTheOperatorThroughPanelSMTP(t *testing.T) {
	renewalEnv(t)
	panelAccount(t)
	wait := captureAlerts(t)

	recordFailure("example.com", errors.New("dns-01 challenge timed out"))

	got := wait(1)
	if len(got) != 1 {
		t.Fatalf("sent %d alerts, want exactly 1", len(got))
	}
	if got[0].to != "alerts@panel.example" {
		t.Errorf("addressed to %q, want the panel's own address", got[0].to)
	}
	if !strings.Contains(got[0].subject, "example.com") {
		t.Errorf("subject %q does not name the domain", got[0].subject)
	}
	if !strings.Contains(got[0].msg, "dns-01 challenge timed out") {
		t.Errorf("body does not carry the reason:\n%s", got[0].msg)
	}
}

// A renewal retries on a timer. An operator who gets the same email hourly for
// three weeks has a filter rule, not an alert, so only the first one goes out.
func TestFailedRenewal_EmailsOnlyTheFirstFailure(t *testing.T) {
	renewalEnv(t)
	panelAccount(t)
	wait := captureAlerts(t)

	recordFailure("example.com", errors.New("first"))
	recordFailure("example.com", errors.New("second"))
	recordFailure("example.com", errors.New("third"))

	if got := wait(1); len(got) != 1 {
		t.Fatalf("sent %d alerts for one failing domain, want 1", len(got))
	}
}

// A fresh install has no panel account and is not broken for it.
func TestFailedRenewal_SendsNothingWithoutAPanelAccount(t *testing.T) {
	renewalEnv(t)
	sent := false
	old := alertSender
	alertSender = func(config.SMTPSettings, string, string, string) error { sent = true; return nil }
	t.Cleanup(func() { alertSender = old })

	recordFailure("example.com", os.ErrPermission)
	time.Sleep(200 * time.Millisecond)

	if sent {
		t.Error("an install with no panel SMTP tried to send an alert")
	}
}
