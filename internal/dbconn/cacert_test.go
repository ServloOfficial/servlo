package dbconn

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
	"testing"
	"time"
)

// samplePEM is a certificate of the shape a managed provider hands over, built
// here rather than checked in so the test carries no expiry of its own.
func samplePEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "managed-db-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// The certificate is stored where servlo keeps its own configuration, not
// anywhere near a site tree: a site is served by nginx and cloned from git, and
// a file in one is a misconfigured location block away from being downloadable.
func TestSaveCACert_StoresItOwnerOnlyOutsideAnySite(t *testing.T) {
	isolate(t)

	path, err := SaveCACert("managed", samplePEM(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, caCertDir()) {
		t.Errorf("stored at %q, want it under %q", path, caCertDir())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("mode = %04o, want 0600", mode)
	}
	dir, err := os.Stat(caCertDir())
	if err != nil {
		t.Fatal(err)
	}
	if mode := dir.Mode().Perm(); mode != 0700 {
		t.Errorf("directory mode = %04o, want 0700", mode)
	}
}

// Storing a second copy for the same connection replaces the first rather than
// leaving two certificates and no way to tell which one is in use.
func TestSaveCACert_ReplacesTheOneAlreadyThere(t *testing.T) {
	isolate(t)

	first, err := SaveCACert("managed", samplePEM(t))
	if err != nil {
		t.Fatal(err)
	}
	second := samplePEM(t)
	path, err := SaveCACert("managed", second)
	if err != nil {
		t.Fatal(err)
	}
	if path != first {
		t.Errorf("path = %q, want the same file as before (%q)", path, first)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(second) {
		t.Error("the file still holds the certificate that was replaced")
	}
}

// A file that is not a certificate is refused with a message that says so. The
// alternative is a connection that stores it happily and fails at the first
// TLS handshake with an error from the driver about an empty pool.
func TestSaveCACert_RefusesWhatIsNotAPEMCertificate(t *testing.T) {
	isolate(t)

	cases := map[string][]byte{
		"an empty file":     []byte(""),
		"a DigitalOcean UI": []byte("<html><body>Download</body></html>"),
		"a private key":     []byte("-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----\n"),
		"a PEM header with rubbish inside": []byte(
			"-----BEGIN CERTIFICATE-----\naGVsbG8gdGhlcmU=\n-----END CERTIFICATE-----\n"),
	}
	for name, data := range cases {
		_, err := SaveCACert("managed", data)
		if err == nil {
			t.Errorf("%s was accepted as a CA certificate", name)
			continue
		}
		if !strings.Contains(err.Error(), "certificate") {
			t.Errorf("%s: error = %q, does not say what was wrong", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(caCertDir(), "managed.crt")); err == nil {
		t.Error("a refused certificate was written anyway")
	}
}

// A private key is the mistake worth naming: the provider's page offers both,
// and "not a certificate" leaves an operator staring at a file they think is
// the right one.
func TestSaveCACert_SaysWhenItWasHandedAPrivateKey(t *testing.T) {
	isolate(t)

	_, err := SaveCACert("managed", []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----\n"))
	if err == nil {
		t.Fatal("a private key was accepted")
	}
	if !strings.Contains(err.Error(), "private key") {
		t.Errorf("error = %q, does not name the private key", err)
	}
}

// The CLI takes a path; the panel takes an upload. Both end in the same place,
// so an operator who added a connection one way can be told where the file is
// the other way.
func TestImportCACert_ReadsTheProvidersFileFromDisk(t *testing.T) {
	isolate(t)

	src := filepath.Join(t.TempDir(), "ca-certificate.crt")
	want := samplePEM(t)
	if err := os.WriteFile(src, want, 0644); err != nil {
		t.Fatal(err)
	}

	path, err := ImportCACert("managed", src)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Error("the imported file is not the one that was named")
	}

	if _, err := ImportCACert("managed", filepath.Join(t.TempDir(), "nope.crt")); err == nil {
		t.Error("a path that is not there was accepted")
	}
}

// A connection name is a path segment here, so anything that could climb out of
// the directory is refused before it becomes one.
func TestSaveCACert_RefusesANameThatIsNotAConnectionName(t *testing.T) {
	isolate(t)

	for _, name := range []string{"", "../escape", "a/b", strings.Repeat("x", 60)} {
		if _, err := SaveCACert(name, samplePEM(t)); err == nil {
			t.Errorf("%q was accepted as a connection name", name)
		}
	}
}

// Removing a connection takes its certificate with it, and removing one that
// has none is not an error: the connection may never have had a certificate.
func TestRemoveCACert_ForgetsTheFile(t *testing.T) {
	isolate(t)

	path, err := SaveCACert("managed", samplePEM(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveCACert("managed"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the certificate is still on disk")
	}
	if err := RemoveCACert("managed"); err != nil {
		t.Errorf("removing a certificate that is not there failed: %v", err)
	}
}
