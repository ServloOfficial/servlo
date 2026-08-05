package certs

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

// acmeEnv points servlo's data home at a temp dir and returns the webroot the
// fake CA fetches challenges from, so the token path under test is the same one
// nginx is configured to serve.
func acmeEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	return config.ACMEChallengeDir()
}

func newTestIssuer(t *testing.T, ca *fakeCA) Issuer {
	t.Helper()
	return NewACMEIssuer(ACMEConfig{DirectoryURL: ca.directoryURL(), Email: "ops@example.com"})
}

// The whole point of the story: a real client, a real order, a real challenge
// fetched over HTTP from the webroot, and a certificate on disk at the end.
func TestACMEIssuer_IssuesThroughHTTP01(t *testing.T) {
	webroot := acmeEnv(t)
	ca := newFakeCA(t, webroot)
	withIssuer(t, newTestIssuer(t, ca))

	dir := t.TempDir()
	if err := IssueCertForce("example.com", []string{"example.com", "www.example.com"}, dir); err != nil {
		t.Fatalf("IssueCertForce: %v", err)
	}

	if len(ca.fetched) != 2 {
		t.Errorf("the CA fetched %d challenges, want one per domain: %v", len(ca.fetched), ca.fetched)
	}
	got := ca.issuedNames(t)
	if strings.Join(got, ",") != "example.com,www.example.com" {
		t.Errorf("certificate covers %v, want both requested domains", got)
	}

	// What landed on disk has to be the certificate the CA signed, parseable,
	// and paired with the key that was generated for it.
	certPEM, err := os.ReadFile(filepath.Join(dir, "example.com.crt"))
	if err != nil {
		t.Fatalf("reading the issued cert: %v", err)
	}
	block, rest := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("the cert file is not a PEM certificate: %q", certPEM)
	}
	if len(rest) == 0 {
		t.Error("only the leaf was written; nginx needs the issuer chain with it or clients that lack the intermediate fail")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parsing the issued leaf: %v", err)
	}

	keyPEM, err := os.ReadFile(filepath.Join(dir, "example.com.key"))
	if err != nil {
		t.Fatalf("reading the issued key: %v", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatalf("the key file is not PEM: %q", keyPEM)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("parsing the issued key: %v", err)
	}
	if !key.PublicKey.Equal(leaf.PublicKey) {
		t.Error("the key on disk does not match the certificate; nginx would refuse to start the site")
	}
}

// A private key is readable only by the user servlo runs as, and so is the
// account key, which is the credential the whole ACME account rests on.
func TestACMEIssuer_KeysAreNotWorldReadable(t *testing.T) {
	webroot := acmeEnv(t)
	ca := newFakeCA(t, webroot)
	withIssuer(t, newTestIssuer(t, ca))

	dir := t.TempDir()
	if err := IssueCertForce("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("IssueCertForce: %v", err)
	}

	for _, path := range []string{
		filepath.Join(dir, "example.com.key"),
		accountKeyPath(t, ca),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s mode = %#o, want no group or other access", path, perm)
		}
	}
}

func accountKeyPath(t *testing.T, ca *fakeCA) string {
	t.Helper()
	host, err := directoryHost(ca.directoryURL())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(config.ACMEAccountDir(host), "account.key")
}

// The account key is the ACME account. Regenerating it on every issuance would
// silently register a new account each time and throw away the rate-limit and
// expiry-notice history attached to the old one.
func TestACMEIssuer_ReusesTheAccountKey(t *testing.T) {
	webroot := acmeEnv(t)
	ca := newFakeCA(t, webroot)
	withIssuer(t, newTestIssuer(t, ca))

	dir := t.TempDir()
	if err := IssueCertForce("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("first issue: %v", err)
	}
	first, err := os.ReadFile(accountKeyPath(t, ca))
	if err != nil {
		t.Fatalf("reading the account key: %v", err)
	}

	if err := IssueCertForce("other.example", []string{"other.example"}, dir); err != nil {
		t.Fatalf("second issue: %v", err)
	}
	second, err := os.ReadFile(accountKeyPath(t, ca))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("the account key was regenerated on the second issuance")
	}
}

// A staging account and a production account are different accounts. Sharing
// one key between them fails registration in a way that reads like a corrupt
// key, so they get separate directories.
func TestACMEIssuer_StagingAndProductionKeepSeparateAccounts(t *testing.T) {
	prodHost, err := directoryHost(LetsEncryptProduction)
	if err != nil {
		t.Fatal(err)
	}
	stagingHost, err := directoryHost(LetsEncryptStaging)
	if err != nil {
		t.Fatal(err)
	}
	if prodHost == stagingHost {
		t.Fatalf("staging and production resolve to the same account directory %q", prodHost)
	}
}

// The refusal has to name the domain and say what failed, because "could not
// issue" with no cause is the difference between a five-minute DNS fix and an
// afternoon.
func TestACMEIssuer_FailedValidationLeavesNoCertAndSaysWhy(t *testing.T) {
	webroot := acmeEnv(t)
	ca := newFakeCA(t, webroot)
	ca.failValidation = true
	withIssuer(t, newTestIssuer(t, ca))

	dir := t.TempDir()
	err := IssueCertForce("example.com", []string{"example.com"}, dir)
	if err == nil {
		t.Fatal("a failed authorization reported success")
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Errorf("error %q does not name the domain that failed", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "example.com.crt")); statErr == nil {
		t.Error("a certificate was written despite the authorization failing")
	}
}

// Tokens are public files under a directory nginx serves to anyone. Leaving
// them behind accumulates litter and, worse, makes a stale token from a failed
// attempt indistinguishable from the live one on the next try.
func TestACMEIssuer_CleansUpChallengeTokens(t *testing.T) {
	webroot := acmeEnv(t)
	ca := newFakeCA(t, webroot)
	withIssuer(t, newTestIssuer(t, ca))

	dir := t.TempDir()
	if err := IssueCertForce("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("IssueCertForce: %v", err)
	}
	assertNoTokensLeft(t, webroot)

	ca.failValidation = true
	if err := IssueCertForce("example.com", []string{"example.com"}, dir); err == nil {
		t.Fatal("expected the failing issue to error")
	}
	assertNoTokensLeft(t, webroot)
}

func assertNoTokensLeft(t *testing.T, webroot string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(webroot, ".well-known", "acme-challenge"))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("challenge token %q was left behind", e.Name())
	}
}

// The reissue window has to work against a certificate the CA actually signed,
// not just a hand-built one: a 90-day Let's Encrypt leaf is inside the 30-day
// window for its last month and must be reissued then, and left alone before.
func TestACMEIssuer_ReissueWindowAppliesToARealLeaf(t *testing.T) {
	webroot := acmeEnv(t)
	ca := newFakeCA(t, webroot)
	withIssuer(t, newTestIssuer(t, ca))
	dir := t.TempDir()

	if err := IssueCert("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("first issue: %v", err)
	}
	fetchedAfterFirst := len(ca.fetched)

	// A fresh 90-day leaf is well clear of the window, so nothing happens.
	if err := IssueCert("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("second issue: %v", err)
	}
	if len(ca.fetched) != fetchedAfterFirst {
		t.Errorf("a fresh 90-day certificate was reissued; the renewal check is not cheap")
	}

	// Sign the next one close to expiry and it falls inside the window.
	ca.notAfter = time.Now().Add(10 * 24 * time.Hour)
	if err := IssueCertForce("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("reissue near expiry: %v", err)
	}
	if err := IssueCert("example.com", []string{"example.com"}, dir); err != nil {
		t.Fatalf("renewal pass: %v", err)
	}
	if len(ca.fetched) <= fetchedAfterFirst+1 {
		t.Error("a certificate inside the reissue window was not renewed")
	}
}

func TestACMEIssuer_NameIdentifiesTheDirectory(t *testing.T) {
	prod := NewACMEIssuer(ACMEConfig{DirectoryURL: LetsEncryptProduction})
	staging := NewACMEIssuer(ACMEConfig{DirectoryURL: LetsEncryptStaging})
	if prod.Name() == staging.Name() {
		t.Errorf("staging and production both report %q; doctor output could not tell them apart", prod.Name())
	}
	if !strings.Contains(staging.Name(), "staging") {
		t.Errorf("the staging issuer reports %q, which does not say it is staging", staging.Name())
	}
}
