package certs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/auditlog"
)

func renewalEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	return tmp
}

// A failed renewal that nobody hears about is the failure mode this story
// exists for: the certificate keeps working for another month, then the site
// goes down with no warning anyone acted on.
func TestFailedRenewal_IsRecordedInTheAuditLog(t *testing.T) {
	renewalEnv(t)
	withIssuer(t, &recordingIssuer{name: "failing", err: os.ErrPermission})
	withDNSReport(t, readyReport("example.com"))

	if err := IssueCertForce("example.com", []string{"example.com"}, t.TempDir()); err == nil {
		t.Fatal("a failing issuer reported success")
	}

	entries, err := auditlog.Recent(10)
	if err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("a failed renewal left no audit entry")
	}
	got := entries[0]
	if got.Action != "cert.issue.failed" {
		t.Errorf("action = %q, want cert.issue.failed", got.Action)
	}
	if got.Subject != "example.com" {
		t.Errorf("subject = %q, want the domain", got.Subject)
	}
	if got.Detail == "" {
		t.Error("the entry does not say why it failed")
	}
}

// A success after a failure has to clear the alarm, or the banner outlives the
// problem and the operator learns to ignore it.
func TestSuccessfulIssuance_ClearsThePreviousFailure(t *testing.T) {
	renewalEnv(t)
	dir := t.TempDir()
	withDNSReport(t, readyReport("example.com"))

	withIssuer(t, &recordingIssuer{name: "failing", err: os.ErrPermission})
	_ = IssueCertForce("example.com", []string{"example.com"}, dir)
	if len(RenewalFailures()) != 1 {
		t.Fatalf("a failed issuance did not raise an alarm: %v", RenewalFailures())
	}

	withIssuer(t, &recordingIssuer{name: "working"})
	if err := IssueCertForce("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("IssueCertForce: %v", err)
	}
	if failures := RenewalFailures(); len(failures) != 0 {
		t.Errorf("the alarm survived a successful issuance: %v", failures)
	}
}

// The banner needs the domain, when it started failing, and why. "Renewal
// failed" on its own tells an operator nothing they can act on.
func TestRenewalFailure_CarriesWhatTheOperatorNeeds(t *testing.T) {
	renewalEnv(t)
	withIssuer(t, &recordingIssuer{name: "failing", err: os.ErrPermission})
	withDNSReport(t, readyReport("example.com"))

	_ = IssueCertForce("example.com", []string{"example.com"}, t.TempDir())

	failures := RenewalFailures()
	if len(failures) != 1 {
		t.Fatalf("failures = %v, want one", failures)
	}
	f := failures[0]
	if f.Domain != "example.com" {
		t.Errorf("domain = %q", f.Domain)
	}
	if f.Reason == "" {
		t.Error("the failure carries no reason")
	}
	if f.Since.IsZero() {
		t.Error("the failure does not say when it started")
	}
}

// Repeated failures keep the original start time. A renewal that has been
// failing for three weeks is a different problem from one that failed once, and
// resetting the clock on every attempt hides the difference.
func TestRenewalFailure_KeepsTheFirstFailureTime(t *testing.T) {
	renewalEnv(t)
	withIssuer(t, &recordingIssuer{name: "failing", err: os.ErrPermission})
	withDNSReport(t, readyReport("example.com"))
	dir := t.TempDir()

	_ = IssueCertForce("example.com", []string{"example.com"}, dir)
	first := RenewalFailures()[0].Since

	time.Sleep(10 * time.Millisecond)
	_ = IssueCertForce("example.com", []string{"example.com"}, dir)

	if got := RenewalFailures()[0].Since; !got.Equal(first) {
		t.Errorf("the failure start moved from %v to %v", first, got)
	}
}

// ── Never serving an expired certificate silently ────────────────────────────

// The certificate on disk expiring is the end state of a renewal that has been
// failing, and it is the one that must never be quiet.
func TestExpiredCertificate_IsReportedEvenWithNoRecordedFailure(t *testing.T) {
	tmp := renewalEnv(t)
	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeLeafCert(t, filepath.Join(certsDir, "example.com.crt"), time.Now().Add(-24*time.Hour))

	problems := ExpiryProblems([]string{"example.com"})
	if len(problems) != 1 {
		t.Fatalf("an expired certificate produced %d problems, want 1", len(problems))
	}
	if !strings.Contains(problems[0].Reason, "expired") {
		t.Errorf("reason = %q, want it to say the certificate expired", problems[0].Reason)
	}
}

// Inside the renewal window but not yet expired is a warning, not a crisis:
// the self-heal has three more weeks of attempts before it matters.
func TestExpiringSoon_IsReportedSeparatelyFromExpired(t *testing.T) {
	tmp := renewalEnv(t)
	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeLeafCert(t, filepath.Join(certsDir, "example.com.crt"), time.Now().Add(5*24*time.Hour))

	problems := ExpiryProblems([]string{"example.com"})
	if len(problems) != 1 {
		t.Fatalf("a nearly-expired certificate produced %d problems, want 1", len(problems))
	}
	if problems[0].Expired {
		t.Error("a certificate five days from expiry was reported as already expired")
	}
}

func TestHealthyCertificate_IsNotAProblem(t *testing.T) {
	tmp := renewalEnv(t)
	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeLeafCert(t, filepath.Join(certsDir, "example.com.crt"), time.Now().Add(80*24*time.Hour))

	if problems := ExpiryProblems([]string{"example.com"}); len(problems) != 0 {
		t.Errorf("a healthy certificate was reported as a problem: %v", problems)
	}
}

// A secured site whose certificate file has gone missing is the loudest case of
// all: nginx will not start the site.
func TestMissingCertificate_IsAProblem(t *testing.T) {
	renewalEnv(t)
	problems := ExpiryProblems([]string{"example.com"})
	if len(problems) != 1 {
		t.Fatalf("a missing certificate produced %d problems, want 1", len(problems))
	}
	if !problems[0].Expired {
		t.Error("a missing certificate should be treated as urgently as an expired one")
	}
}
