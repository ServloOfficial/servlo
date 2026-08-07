package certs

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

// The panel's own certificate, and why it is self-signed.
//
// Every site on the box gets a real certificate from Let's Encrypt, and the
// panel eventually does too, through the same flow, once a domain points at it.
// Before that there is nothing to point at: a fresh droplet has an address and
// no DNS, and the first thing an operator does on it is type a password.
//
// So the panel signs a leaf for its own addresses and serves that. It is not
// trusted, and the browser says so; what it buys is that the login does not
// cross the internet in clear text while DNS propagates. Nothing is installed
// into any trust store, on this machine or any other: that would be the local
// CA S3.1 deleted, and it has no successor here. Accepting the warning once, or
// replacing it with a real certificate, are the two intended paths.

// panelCertValidity is deliberately long. A self-signed certificate an operator
// accepted in their browser is pinned by that acceptance, so replacing it costs
// them the same click again; the short lifetimes that make sense for a public
// certificate buy nothing here.
const panelCertValidity = 5 * 365 * 24 * time.Hour

// panelRenewBefore reissues while the old certificate still works, so the
// replacement is never the thing an operator discovers by being locked out.
const panelRenewBefore = 30 * 24 * time.Hour

// cryptoRandReader is the entropy source, named so tests can reach the same one.
var cryptoRandReader io.Reader = rand.Reader

var panelCertMu sync.Mutex

// PanelCertPath and PanelKeyPath are where the panel's own pair lives, beside
// the site certificates rather than among them: nothing scans this one for
// renewal, because there is no authority to renew it with.
func PanelCertPath() string { return filepath.Join(config.CertsDir(), "panel", "panel.crt") }
func PanelKeyPath() string  { return filepath.Join(config.CertsDir(), "panel", "panel.key") }

// PanelCertificate returns the panel's TLS certificate, generating one when
// there is none, when the addresses or domain it covers have changed, or when
// the stored one is close enough to expiry to be worth replacing.
//
// addresses are this server's own addresses; domain is the panel's FQDN, empty
// until one is attached. Loopback and localhost are always included so reaching
// the panel through an SSH tunnel does not add a name mismatch to the
// untrusted-issuer warning already on screen.
func PanelCertificate(addresses []net.IP, domain string) (*tls.Certificate, error) {
	panelCertMu.Lock()
	defer panelCertMu.Unlock()

	ips, names := panelSubjects(addresses, domain)
	if cert, ok := loadPanelCertificate(ips, names); ok {
		return cert, nil
	}
	return generatePanelCertificate(ips, names)
}

// panelSubjects returns the addresses and names the certificate must cover,
// deduplicated and ordered, so the same inputs always produce the same set and
// a reissue is decided by what changed rather than by argument order.
func panelSubjects(addresses []net.IP, domain string) ([]net.IP, []string) {
	seen := map[string]net.IP{}
	add := func(ip net.IP) {
		if ip == nil {
			return
		}
		seen[ip.String()] = ip
	}
	add(net.IPv4(127, 0, 0, 1))
	add(net.IPv6loopback)
	for _, ip := range addresses {
		add(ip)
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ips := make([]net.IP, 0, len(keys))
	for _, k := range keys {
		ips = append(ips, seen[k])
	}

	names := []string{"localhost"}
	if domain != "" && domain != "localhost" {
		names = append(names, domain)
	}
	return ips, names
}

// loadPanelCertificate returns the stored pair when it still covers everything
// asked of it and is not near expiry. Any reason to doubt it is a reason to
// mint a new one: a certificate is cheap and being locked out of the panel is
// not.
func loadPanelCertificate(ips []net.IP, names []string) (*tls.Certificate, bool) {
	cert, err := tls.LoadX509KeyPair(PanelCertPath(), PanelKeyPath())
	if err != nil {
		return nil, false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, false
	}
	if time.Now().After(leaf.NotAfter.Add(-panelRenewBefore)) {
		return nil, false
	}
	for _, ip := range ips {
		if leaf.VerifyHostname(ip.String()) != nil {
			return nil, false
		}
	}
	for _, name := range names {
		if leaf.VerifyHostname(name) != nil {
			return nil, false
		}
	}
	cert.Leaf = leaf
	return &cert, true
}

func generatePanelCertificate(ips []net.IP, names []string) (*tls.Certificate, error) {
	if err := os.MkdirAll(filepath.Dir(PanelCertPath()), 0700); err != nil {
		return nil, fmt.Errorf("creating the panel certificate directory: %w", err)
	}
	// The key is reused across reissues so an operator who exported the
	// fingerprint of the public key keeps a stable one, and rewritten when
	// absent. loadOrCreateAccountKey already does exactly this, 0600.
	key, err := loadOrCreateAccountKey(PanelKeyPath())
	if err != nil {
		return nil, fmt.Errorf("reading the panel key: %w", err)
	}

	der, err := signPanelLeaf(key, ips, names)
	if err != nil {
		return nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(PanelCertPath(), pemBytes, 0644); err != nil {
		return nil, fmt.Errorf("writing the panel certificate: %w", err)
	}

	cert, err := tls.LoadX509KeyPair(PanelCertPath(), PanelKeyPath())
	if err != nil {
		return nil, fmt.Errorf("loading the panel certificate that was just written: %w", err)
	}
	return &cert, nil
}

func signPanelLeaf(key *ecdsa.PrivateKey, ips []net.IP, names []string) ([]byte, error) {
	serial, err := rand.Int(cryptoRandReader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating a serial number: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Servlo panel", Organization: []string{"Servlo"}},
		// A minute in the past, because a client whose clock is slightly
		// behind the server's would otherwise reject a certificate that was
		// valid the moment it was made.
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(panelCertValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		// Self-signed means the leaf signs itself, which requires the CA bit
		// even though nothing else is ever signed with it. It is not an
		// authority: it is not installed anywhere, and no other certificate
		// chains to it.
		IsCA:        true,
		IPAddresses: ips,
		DNSNames:    names,
	}
	return x509.CreateCertificate(cryptoRandReader, tmpl, tmpl, key.Public(), key)
}
