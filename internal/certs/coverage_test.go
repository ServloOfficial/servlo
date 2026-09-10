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
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
)

// stageSiteCert writes a healthy, long-lived certificate for a site that names
// exactly the given domains, plus the key file beside it that NeedsRenewal
// stats.
func stageSiteCert(t *testing.T, primary string, names []string) {
	t.Helper()
	dir := sitesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: primary},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(80 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(filepath.Join(dir, primary+".crt"), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, primary+".key"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Adding a domain to a secured site reissues its certificate, and the ordinary
// order of work means that reissue usually fails: the operator adds the domain
// first and repoints its DNS second, and the gate refuses to issue for a name
// that does not resolve here yet. So the site is left serving a certificate
// that does not name one of the domains it answers on.
//
// Nothing noticed afterwards. Renewal asked only how close the certificate was
// to expiring, so it said no for another sixty days while every visitor to the
// new domain met a name mismatch, and it went on saying no after the operator
// repointed the DNS, which is the moment it could finally have succeeded.
//
// The panel's own certificate has always been checked this way, against the
// names it has to cover as well as its expiry. A site's is now too.
func TestNeedsRenewal_TrueWhenTheCertificateDoesNotNameEveryDomain(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)

	stageSiteCert(t, "harborlist.example", []string{"harborlist.example"})

	site := config.Site{
		Name:    "harborlist",
		Domains: []string{"harborlist.example", "www.harborlist.example"},
		Secured: true,
	}
	if !NeedsRenewal(site) {
		t.Error("a certificate that does not name www.harborlist.example was treated as healthy")
	}
}

func TestNeedsRenewal_FalseWhenTheCertificateNamesThemAll(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)

	stageSiteCert(t, "harborlist.example", []string{"harborlist.example", "www.harborlist.example"})

	site := config.Site{
		Name:    "harborlist",
		Domains: []string{"harborlist.example", "www.harborlist.example"},
		Secured: true,
	}
	if NeedsRenewal(site) {
		t.Error("a certificate covering every domain was reissued anyway")
	}
}

// The renewal pass is what turns the check into a repair: once the operator
// repoints the DNS, the next pass issues a certificate naming the new domain
// without them having to remember to press anything.
func TestRenewIfDue_ReissuesForTheDomainTheCertificateIsMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	rec := withIssuer(t, &recordingIssuer{name: "test"})

	stageSiteCert(t, "harborlist.example", []string{"harborlist.example"})

	site := config.Site{
		Name:    "harborlist",
		Domains: []string{"harborlist.example", "www.harborlist.example"},
		Secured: true,
	}
	renewed, err := RenewIfDue(site)
	if err != nil {
		t.Fatalf("RenewIfDue: %v", err)
	}
	if !renewed {
		t.Fatal("RenewIfDue left the site on a certificate missing one of its domains")
	}
	if len(rec.calls) != 1 {
		t.Fatalf("issuer called %d times, want 1", len(rec.calls))
	}
	if got := strings.Join(rec.calls[0].domains, ","); got != "harborlist.example,www.harborlist.example" {
		t.Errorf("issuer got domains %q, want both", got)
	}
}

// IssueCert reuses an existing certificate without calling the issuer, and used
// to do so however wide the domain list it was handed. Its own doc says it
// issues one covering all the given domains, so a caller widening that list has
// to get a certificate that covers it.
func TestIssueCert_DoesNotReuseACertificateNarrowerThanAsked(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	rec := withIssuer(t, &recordingIssuer{name: "test"})

	stageSiteCert(t, "harborlist.example", []string{"harborlist.example"})

	err := IssueCert("harborlist.example",
		[]string{"harborlist.example", "shop.harborlist.example"}, sitesDir())
	if err != nil {
		t.Fatalf("IssueCert: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("issuer called %d times, want 1: the narrower certificate was reused", len(rec.calls))
	}
}
