package certs

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"testing"
	"time"
)

func panelTestDirs(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// The panel is the one thing on the box that has to be reachable before any
// domain points at it, so it cannot wait for ACME. It serves a self-signed
// certificate it makes itself, which is not a trusted certificate and is not
// meant to be one: it is what stops a login travelling in clear text over the
// internet while DNS is still propagating.
func TestPanelCertificate_IsGeneratedOnFirstUse(t *testing.T) {
	panelTestDirs(t)

	cert, err := PanelCertificate([]net.IP{net.ParseIP("203.0.113.10")}, "")
	if err != nil {
		t.Fatalf("PanelCertificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parsing the leaf: %v", err)
	}

	if !leaf.IsCA {
		// Not a demand that it be a CA, a demand that it not need one. A
		// self-signed leaf is the whole chain; anything that wanted a local
		// authority to sign it would be the local CA S3.1 deleted.
		if len(cert.Certificate) != 1 {
			t.Errorf("panel certificate has %d certificates, want a single self-signed leaf", len(cert.Certificate))
		}
	}
	if err := leaf.VerifyHostname("203.0.113.10"); err != nil {
		t.Errorf("certificate does not cover the address it was asked for: %v", err)
	}
	// A browser reaching the panel by address needs the address in a SAN;
	// a CN alone has not been accepted for years.
	if len(leaf.IPAddresses) == 0 {
		t.Error("certificate names no IP addresses, so no browser will match it")
	}
}

// The key is a credential like any other. Created 0600, never chmodded after,
// so it is not briefly readable in between.
func TestPanelCertificate_KeyIsOwnerOnly(t *testing.T) {
	panelTestDirs(t)

	if _, err := PanelCertificate([]net.IP{net.ParseIP("203.0.113.10")}, ""); err != nil {
		t.Fatalf("PanelCertificate: %v", err)
	}
	info, err := os.Stat(PanelKeyPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("panel key mode = %04o, want 0600", mode)
	}
}

// Regenerating on every start would change the fingerprint the operator just
// accepted in their browser, which trains them to click through the warning.
func TestPanelCertificate_IsReusedAcrossCalls(t *testing.T) {
	panelTestDirs(t)

	first, err := PanelCertificate([]net.IP{net.ParseIP("203.0.113.10")}, "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := PanelCertificate([]net.IP{net.ParseIP("203.0.113.10")}, "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if string(first.Certificate[0]) != string(second.Certificate[0]) {
		t.Error("a second call minted a new certificate, so the fingerprint changes on every restart")
	}
}

// A droplet that gains a floating IP, or is rebuilt on a new address, would
// otherwise serve a certificate for an address it no longer has.
func TestPanelCertificate_IsReissuedWhenTheAddressesChange(t *testing.T) {
	panelTestDirs(t)

	first, err := PanelCertificate([]net.IP{net.ParseIP("203.0.113.10")}, "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := PanelCertificate([]net.IP{net.ParseIP("203.0.113.10"), net.ParseIP("198.51.100.4")}, "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if string(first.Certificate[0]) == string(second.Certificate[0]) {
		t.Fatal("a new address did not produce a new certificate")
	}
	leaf, err := x509.ParseCertificate(second.Certificate[0])
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if err := leaf.VerifyHostname("198.51.100.4"); err != nil {
		t.Errorf("reissued certificate does not cover the new address: %v", err)
	}
}

// Once a domain points at the panel it belongs in the certificate too, so the
// self-signed one keeps working by name until a real one replaces it.
func TestPanelCertificate_CoversTheDomainWhenOneIsSet(t *testing.T) {
	panelTestDirs(t)

	cert, err := PanelCertificate([]net.IP{net.ParseIP("203.0.113.10")}, "panel.example.com")
	if err != nil {
		t.Fatalf("PanelCertificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if err := leaf.VerifyHostname("panel.example.com"); err != nil {
		t.Errorf("certificate does not cover the panel domain: %v", err)
	}
}

// Loopback is always in the certificate, so reaching the panel over an SSH
// tunnel does not produce a name mismatch on top of the untrusted-issuer
// warning that is already there.
func TestPanelCertificate_AlwaysCoversLoopback(t *testing.T) {
	panelTestDirs(t)

	cert, err := PanelCertificate(nil, "")
	if err != nil {
		t.Fatalf("PanelCertificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	for _, name := range []string{"127.0.0.1", "::1", "localhost"} {
		if err := leaf.VerifyHostname(name); err != nil {
			t.Errorf("certificate does not cover %s: %v", name, err)
		}
	}
}

// An expired certificate is refused outright by every browser, with no
// click-through, so it has to be replaced rather than served.
func TestPanelCertificate_IsReissuedWhenExpired(t *testing.T) {
	panelTestDirs(t)

	if _, err := PanelCertificate(nil, ""); err != nil {
		t.Fatalf("PanelCertificate: %v", err)
	}
	expirePanelCertForTest(t)

	fresh, err := PanelCertificate(nil, "")
	if err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	leaf, err := x509.ParseCertificate(fresh.Certificate[0])
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if !leaf.NotAfter.After(time.Now()) {
		t.Error("the panel is still serving an expired certificate")
	}
}

// expirePanelCertForTest rewrites the stored certificate with one that has
// already expired, leaving its key alone, which is what an install that has
// been running past the validity window looks like.
func expirePanelCertForTest(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(PanelCertPath())
	if err != nil {
		t.Fatalf("reading the panel certificate: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("stored panel certificate is not PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	key, err := loadOrCreateAccountKey(PanelKeyPath())
	if err != nil {
		t.Fatalf("reading the panel key: %v", err)
	}
	tmpl := *leaf
	tmpl.NotBefore = time.Now().Add(-48 * time.Hour)
	tmpl.NotAfter = time.Now().Add(-1 * time.Hour)
	der, err := x509.CreateCertificate(cryptoRandReader, &tmpl, &tmpl, key.Public(), key)
	if err != nil {
		t.Fatalf("re-signing: %v", err)
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(PanelCertPath(), out, 0644); err != nil {
		t.Fatalf("writing: %v", err)
	}
}

// The certificate has to load as a TLS certificate, not merely parse: a key
// and a leaf that do not belong together fail here and nowhere else.
func TestPanelCertificate_LoadsAsATLSCertificate(t *testing.T) {
	panelTestDirs(t)

	if _, err := PanelCertificate(nil, ""); err != nil {
		t.Fatalf("PanelCertificate: %v", err)
	}
	if _, err := tls.LoadX509KeyPair(PanelCertPath(), PanelKeyPath()); err != nil {
		t.Fatalf("the stored pair does not load as a TLS certificate: %v", err)
	}
}
