package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// writeLeafCert writes a self-signed PEM certificate to path whose NotAfter is
// the given time, so the expiry-aware reuse path in IssueCert has a real cert
// to parse.
//
// It names the domain the file is stored under, which is how servlo names a
// site's certificate. A certificate carrying no SAN at all names no host as far
// as crypto/x509 is concerned, so a fixture without one would stand for a
// certificate no browser would accept rather than for the healthy one these
// tests mean.
func writeLeafCert(t *testing.T, path string, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	domain := strings.TrimSuffix(filepath.Base(path), ".crt")
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		DNSNames:     []string{domain},
		NotBefore:    notAfter.Add(-2 * 365 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0644); err != nil {
		t.Fatal(err)
	}
}

// ── CertExists ────────────────────────────────────────────────────────────────

func TestCertExists_returnsFalseWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	if CertExists("myapp.example") {
		t.Error("expected false for non-existent cert")
	}
}

func TestCertExists_returnsTrueWhenPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	// Create the expected cert file path
	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	os.MkdirAll(certsDir, 0755)
	os.WriteFile(filepath.Join(certsDir, "myapp.example.crt"), []byte("fake cert"), 0644)

	if !CertExists("myapp.example") {
		t.Error("expected true when cert file exists")
	}
}

func TestCertExists_onlyCrtRequired(t *testing.T) {
	// CertExists checks for .crt only, not .key
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	os.MkdirAll(certsDir, 0755)
	// .crt exists, no .key
	os.WriteFile(filepath.Join(certsDir, "site.example.crt"), []byte("fake cert"), 0644)

	if !CertExists("site.example") {
		t.Error("expected true when only .crt file exists")
	}
}

// ── IssueCert vs IssueCertForce semantics ────────────────────────────────────

// IssueCert is documented as a no-op when the cert and key already exist on
// disk and the cert is clear of the reissue window. Pins the contract so
// callers that mutate the SAN list (domain add, edit, remove) keep using
// IssueCertForce; using IssueCert would silently preserve a stale cert and the
// browser would reject the new hostname with ERR_CERT_AUTHORITY_INVALID.
// Regression test for the bug where adding a secondary domain to a secured site
// never widened the cert's SAN list.
func TestIssueCert_skipsWhenCertExists(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := withIssuer(t, &recordingIssuer{name: "test"})

	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(certsDir, "site.example.crt")
	keyPath := filepath.Join(certsDir, "site.example.key")
	// A valid cert well clear of the reissue window: IssueCert must reuse it.
	writeLeafCert(t, certPath, time.Now().Add(365*24*time.Hour))
	original, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("STALE-KEY"), 0600); err != nil {
		t.Fatal(err)
	}

	// The SAN list it is asked for is the one the cert already covers, which is
	// the case that must not reissue. A wider list is a different question and
	// TestIssueCert_DoesNotReuseACertificateNarrowerThanAsked asks it.
	if err := IssueCert("site.example", []string{"site.example"}, certsDir); err != nil {
		t.Fatalf("IssueCert returned %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("IssueCert called the issuer for a still-valid cert")
	}
	got, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Errorf("IssueCert overwrote a still-valid cert; got %q want %q", got, original)
	}

	// IssueCertForce must go to the issuer with the full domain set and put
	// what it produced in place.
	if err := IssueCertForce("site.example", []string{"site.example", "extra.example"}, certsDir); err != nil {
		t.Fatalf("IssueCertForce returned %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("IssueCertForce called the issuer %d times, want 1", len(rec.calls))
	}
	if strings.Join(rec.calls[0].domains, ",") != "site.example,extra.example" {
		t.Errorf("issuer got domains %v, want the new domain included", rec.calls[0].domains)
	}
	got, err = os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "issued-by-test-cert" {
		t.Errorf("IssueCertForce did not replace the cert; body %q", got)
	}
}

// ── IssueCert expiry-aware reuse ─────────────────────────────────────────────

// A cert within the reissue window must be regenerated by IssueCert even though
// both files exist, so an ordinary start or watcher pass self-heals an aging
// cert instead of serving it until it expires. Regression test for issue #729.
func TestIssueCert_reissuesCertNearExpiry(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := withIssuer(t, &recordingIssuer{name: "test"})

	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(certsDir, "site.example.crt")
	keyPath := filepath.Join(certsDir, "site.example.key")
	// 10 days from expiry: inside the 30-day window.
	writeLeafCert(t, certPath, time.Now().Add(10*24*time.Hour))
	if err := os.WriteFile(keyPath, []byte("OLD-KEY"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := IssueCert("site.example", []string{"site.example"}, certsDir); err != nil {
		t.Fatalf("IssueCert returned %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("IssueCert did not reissue a cert near expiry; issuer called %d times", len(rec.calls))
	}
	got, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "issued-by-test-cert" {
		t.Errorf("the aging cert was not replaced; body %q", got)
	}
}

// An already-expired cert must be reissued the same way.
func TestIssueCert_reissuesExpiredCert(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := withIssuer(t, &recordingIssuer{name: "test"})

	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(certsDir, "site.example.crt")
	keyPath := filepath.Join(certsDir, "site.example.key")
	writeLeafCert(t, certPath, time.Now().Add(-24*time.Hour))
	if err := os.WriteFile(keyPath, []byte("OLD-KEY"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := IssueCert("site.example", []string{"site.example"}, certsDir); err != nil {
		t.Fatalf("IssueCert returned %v", err)
	}
	if len(rec.calls) != 1 {
		t.Errorf("IssueCert did not reissue an expired cert; issuer called %d times", len(rec.calls))
	}
}

// A cert comfortably clear of the window must be reused untouched.
func TestIssueCert_reusesCertFarFromExpiry(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := withIssuer(t, &recordingIssuer{name: "test"})

	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(certsDir, "site.example.crt")
	keyPath := filepath.Join(certsDir, "site.example.key")
	writeLeafCert(t, certPath, time.Now().Add(200*24*time.Hour))
	original, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("OLD-KEY"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := IssueCert("site.example", []string{"site.example"}, certsDir); err != nil {
		t.Fatalf("IssueCert returned %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("IssueCert reissued a cert far from expiry")
	}
	got, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Errorf("IssueCert reissued a cert far from expiry; got %q want %q", got, original)
	}
}

// An unreadable / non-PEM cert on disk must fall through to reissue rather than
// being trusted, so a corrupt cert can't pin the site to a broken TLS state.
func TestIssueCert_reissuesUnparseableCert(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := withIssuer(t, &recordingIssuer{name: "test"})

	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(certsDir, "site.example.crt")
	keyPath := filepath.Join(certsDir, "site.example.key")
	if err := os.WriteFile(certPath, []byte("NOT-A-PEM-CERT"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("OLD-KEY"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := IssueCert("site.example", []string{"site.example"}, certsDir); err != nil {
		t.Fatalf("IssueCert returned %v", err)
	}
	if len(rec.calls) != 1 {
		t.Errorf("IssueCert did not reissue an unparseable cert; issuer called %d times", len(rec.calls))
	}
}

// ── IssueCertForce concurrency ───────────────────────────────────────────────

// TestIssueCertForce_concurrentCallsDontCollide pins the fix for the shared
// .new tempfile race: two parallel IssueCertForce calls for the same domain
// (e.g. two reconcile passes firing on the same
// site) must not interleave their renames. Pre-fix both writers used a
// fixed "<primary>.crt.new" path; one would clobber the other's tempfile
// mid-write or rename a partially-flushed file. The fix uses a unique
// tempfile per goroutine.
func TestIssueCertForce_concurrentCallsDontCollide(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	// A slow issuer widens the race window well beyond rename atomicity.
	withIssuer(t, issuerFunc(func(_ string, domains []string, certPath, keyPath string) error {
		time.Sleep(50 * time.Millisecond)
		if err := os.WriteFile(certPath, []byte(strings.Join(domains, " ")), 0644); err != nil {
			return err
		}
		return os.WriteFile(keyPath, []byte("FAKE-KEY"), 0600)
	}))

	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := IssueCertForce("alpha.example", []string{"alpha.example"}, certsDir)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Errorf("concurrent IssueCertForce returned error: %v", e)
		}
	}

	// Cert must exist and contain a complete write; truncation or interleaving
	// would break the content.
	body, err := os.ReadFile(filepath.Join(certsDir, "alpha.example.crt"))
	if err != nil {
		t.Fatalf("cert missing after concurrent issue: %v", err)
	}
	if string(body) != "alpha.example" {
		t.Errorf("cert content corrupted by concurrent rename; got %q", body)
	}

	// No leftover temp files: each goroutine should have cleaned up its
	// own .new.* paths even on the rename-loser side.
	entries, _ := os.ReadDir(certsDir)
	for _, e := range entries {
		name := e.Name()
		if name == "alpha.example.crt" || name == "alpha.example.key" {
			continue
		}
		if strings.Contains(name, ".new") {
			t.Errorf("leftover temp file %q after concurrent issue", name)
		}
	}
}

// ── IssueCertForce atomicity ─────────────────────────────────────────────────

// TestIssueCertForce_keyRenameFailureRollsBackCert pins the cert/key
// pair atomicity guarantee: when the key rename fails after the cert
// rename succeeded, the previous cert is restored so we don't leave a
// new-cert + old-key mismatch that nginx refuses to load. Failure is
// triggered by having the issuer emit a directory at the key tempfile path,
// since POSIX rename(directory, regular file) gives ENOTDIR.
func TestIssueCertForce_keyRenameFailureRollsBackCert(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	withIssuer(t, issuerFunc(func(_ string, _ []string, certPath, keyPath string) error {
		if err := os.WriteFile(certPath, []byte("NEW-CERT"), 0644); err != nil {
			return err
		}
		return os.MkdirAll(keyPath, 0700)
	}))

	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(certsDir, "myapp.example.crt")
	keyPath := filepath.Join(certsDir, "myapp.example.key")
	if err := os.WriteFile(certPath, []byte("OLD-CERT"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("OLD-KEY"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := IssueCertForce("myapp.example", []string{"myapp.example"}, certsDir); err == nil {
		t.Fatal("expected error when key rename fails, got nil")
	}

	gotCert, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("cert missing after rollback: %v", err)
	}
	if string(gotCert) != "OLD-CERT" {
		t.Errorf("cert not rolled back; got %q, want OLD-CERT", gotCert)
	}
	gotKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("key missing after rollback: %v", err)
	}
	if string(gotKey) != "OLD-KEY" {
		t.Errorf("key shouldn't have changed; got %q, want OLD-KEY", gotKey)
	}
}

// IssueCertForce must leave the existing cert/key intact when issuance fails,
// otherwise a transient error would trip RepairVhosts into flipping the site
// to plain HTTP on the next start.
func TestIssueCertForce_failureLeavesExistingCertIntact(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	withIssuer(t, &recordingIssuer{name: "failing", err: os.ErrPermission})

	certsDir := filepath.Join(tmp, "servlo", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(certsDir, "myapp.example.crt")
	keyPath := filepath.Join(certsDir, "myapp.example.key")
	originalCert := []byte("EXISTING-CERT-PEM")
	originalKey := []byte("EXISTING-KEY-PEM")
	if err := os.WriteFile(certPath, originalCert, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, originalKey, 0600); err != nil {
		t.Fatal(err)
	}

	err := IssueCertForce("myapp.example", []string{"myapp.example", "feat-x.myapp.example"}, certsDir)
	if err == nil {
		t.Fatal("expected error when issuance fails, got nil")
	}

	gotCert, readErr := os.ReadFile(certPath)
	if readErr != nil {
		t.Fatalf("existing cert was deleted after failure: %v", readErr)
	}
	if string(gotCert) != string(originalCert) {
		t.Errorf("cert overwritten on failure path; got %q want %q", gotCert, originalCert)
	}
	gotKey, readErr := os.ReadFile(keyPath)
	if readErr != nil {
		t.Fatalf("existing key was deleted after failure: %v", readErr)
	}
	if string(gotKey) != string(originalKey) {
		t.Errorf("key overwritten on failure path")
	}

	// Temp paths must be cleaned up too — leftover .new files would
	// confuse a subsequent successful reissue.
	entries, _ := os.ReadDir(certsDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".new") {
			t.Errorf("leftover temp file %q after a failed issue", e.Name())
		}
	}
}

// leafPEM returns a self-signed certificate as PEM text, for tests that need a
// body IssueCert's reissue window can actually parse.
func leafPEM(t *testing.T, domain string, notAfter time.Time) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), domain+".crt")
	writeLeafCert(t, path, notAfter)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
