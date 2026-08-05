package certs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

// recordingIssuer stands in for a real one and writes recognisable bytes, so a
// test can tell that what the issuer produced is what ended up on disk.
type recordingIssuer struct {
	name    string
	calls   []issueCall
	err     error
	content string
}

type issueCall struct {
	primary string
	domains []string
}

func (r *recordingIssuer) Name() string { return r.name }

func (r *recordingIssuer) Issue(primary string, domains []string, certPath, keyPath string) error {
	r.calls = append(r.calls, issueCall{primary: primary, domains: append([]string(nil), domains...)})
	if r.err != nil {
		return r.err
	}
	body := r.content
	if body == "" {
		body = "issued-by-" + r.name
	}
	certBody, keyBody := body+"-cert", body+"-key"
	if strings.Contains(body, "BEGIN CERTIFICATE") {
		certBody, keyBody = body, body
	}
	if err := os.WriteFile(certPath, []byte(certBody), 0o644); err != nil {
		return err
	}
	return os.WriteFile(keyPath, []byte(keyBody), 0o600)
}

// issuerFunc covers the cases a recordingIssuer cannot express, like producing
// a key at a path that cannot then be renamed.
type issuerFunc func(primary string, domains []string, certPath, keyPath string) error

func (issuerFunc) Name() string { return "func" }

func (f issuerFunc) Issue(primary string, domains []string, certPath, keyPath string) error {
	return f(primary, domains, certPath, keyPath)
}

func withIssuer(t *testing.T, iss Issuer) *recordingIssuer {
	t.Helper()
	prev := activeIssuer
	t.Cleanup(func() { activeIssuer = prev })
	activeIssuer = func() Issuer { return iss }
	r, _ := iss.(*recordingIssuer)
	return r
}

// The atomic swap, the backup and the rollback are the careful part of this
// package and must keep working whoever issues the certificate.
func TestIssueCertForce_PutsWhatTheIssuerProducedInPlace(t *testing.T) {
	rec := withIssuer(t, &recordingIssuer{name: "test"})
	dir := t.TempDir()

	if err := IssueCertForce("example.com", []string{"example.com", "www.example.com"}, dir); err != nil {
		t.Fatalf("IssueCertForce: %v", err)
	}

	if len(rec.calls) != 1 {
		t.Fatalf("issuer called %d times, want 1", len(rec.calls))
	}
	if rec.calls[0].primary != "example.com" {
		t.Errorf("primary = %q, want example.com", rec.calls[0].primary)
	}
	got, err := os.ReadFile(filepath.Join(dir, "example.com.crt"))
	if err != nil {
		t.Fatalf("reading the issued cert: %v", err)
	}
	if string(got) != "issued-by-test-cert" {
		t.Errorf("cert on disk = %q, not what the issuer wrote", got)
	}
	key, err := os.ReadFile(filepath.Join(dir, "example.com.key"))
	if err != nil {
		t.Fatalf("reading the issued key: %v", err)
	}
	if string(key) != "issued-by-test-key" {
		t.Errorf("key on disk = %q, not what the issuer wrote", key)
	}
}

// A private key is readable only by the user servlo runs as.
func TestIssueCertForce_KeyIsNotWorldReadable(t *testing.T) {
	withIssuer(t, &recordingIssuer{name: "test"})
	dir := t.TempDir()

	if err := IssueCertForce("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("IssueCertForce: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "example.com.key"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("key mode = %#o, want no group or other access", perm)
	}
}

// A failed issue must leave the previous certificate serving. Losing it is
// worse than failing to renew: a missing cert trips the vhost repair into
// flipping the site to plain HTTP.
func TestIssueCertForce_FailureLeavesThePreviousCertInPlace(t *testing.T) {
	dir := t.TempDir()
	withIssuer(t, &recordingIssuer{name: "first", content: "old"})
	if err := IssueCertForce("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("seeding the first cert: %v", err)
	}

	withIssuer(t, &recordingIssuer{name: "second", err: os.ErrPermission})
	if err := IssueCertForce("example.com", []string{"example.com"}, dir); err == nil {
		t.Fatal("a failing issuer reported success")
	}

	got, err := os.ReadFile(filepath.Join(dir, "example.com.crt"))
	if err != nil {
		t.Fatalf("the previous cert is gone: %v", err)
	}
	if string(got) != "old-cert" {
		t.Errorf("cert on disk = %q, want the previous one kept", got)
	}
}

// The reissue window is the inherited scanner and has to keep gating issuance:
// a fresh certificate is left alone, so an ordinary start does not reissue
// every site every time.
func TestIssueCert_SkipsAFreshCertificate(t *testing.T) {
	dir := t.TempDir()
	rec := withIssuer(t, &recordingIssuer{name: "test", content: leafPEM(t, time.Now().Add(365*24*time.Hour))})
	if err := IssueCert("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("first issue: %v", err)
	}
	if err := IssueCert("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("second issue: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Errorf("issuer called %d times, want 1: a cert well inside its window was reissued", len(rec.calls))
	}
}

// The issuer in force comes from config on every issuance, so an operator who
// corrects their authority settings does not have to restart the panel for the
// next renewal to use them.
func TestIssuerFromConfig_HonoursTheConfiguredAuthority(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if got := IssuerName(); got != "letsencrypt" {
		t.Errorf("a fresh install issues from %q, want Let's Encrypt production", got)
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.Certs.Staging = true
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	if got := IssuerName(); got != "letsencrypt-staging" {
		t.Errorf("with staging on the issuer is %q, want the staging directory", got)
	}

	// An explicit directory is for a private ACME server, so the staging flag
	// must not quietly override it.
	cfg.Certs.DirectoryURL = "https://acme.internal:14000/dir"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	if got := IssuerName(); !strings.Contains(got, "acme.internal") {
		t.Errorf("an explicit directory was ignored; issuer is %q", got)
	}
}
