package nginx

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/realrashid/servlo/internal/config"
)

// The TLS a secured site is served with, and why each line is what it is.
//
// None of this was set before, which meant every vhost inherited whatever the
// nginx image defaulted to. That is not a stable answer: images still in use
// have offered TLS 1.0 and 1.1. A panel that goes to the trouble of obtaining a
// real certificate should not then serve it over a protocol deprecated in 2021.

// defaultHSTSMaxAge is a year, the conventional value.
const defaultHSTSMaxAge = 31536000

// tlsProtocols. 1.2 is the floor because 1.0 and 1.1 are deprecated and their
// remaining users are bots. 1.3 is offered because it is faster and simpler and
// every current client speaks it.
const tlsProtocols = "TLSv1.2 TLSv1.3"

// tlsCiphers is the Mozilla intermediate list: forward secrecy on every suite,
// AES-GCM and ChaCha20 only, nothing with CBC, RC4, 3DES or MD5 in it. TLS 1.3
// ignores this entirely, since its suites are fixed by the protocol.
const tlsCiphers = "ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:" +
	"ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:" +
	"ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:" +
	"DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384"

// tlsBaseBlock is everything that does not depend on the site or its
// certificate.
//
// ssl_prefer_server_ciphers is off deliberately. The old advice was to impose
// the server's order; the current advice is to let the client choose, because a
// phone without AES hardware is faster and no less safe on ChaCha20 and is the
// only party that knows which it is.
//
// ssl_session_tickets is off because nginx reuses one ticket key for the life
// of the process. Anyone who later obtains that key can decrypt every session
// recorded since it was created, which throws away exactly the forward secrecy
// the cipher list was chosen for.
const tlsBaseBlock = `    ssl_protocols ` + tlsProtocols + `;
    ssl_ciphers ` + tlsCiphers + `;
    ssl_prefer_server_ciphers off;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;
    ssl_session_tickets off;
`

// hstsHeaderFor renders the Strict-Transport-Security header.
//
// `always` matters: without it nginx omits the header on error responses, which
// are the ones an attacker can most easily provoke.
//
// Deliberately without includeSubDomains, which would extend the policy to a
// group secondary left on plain http on purpose and break it in every browser
// that had seen the parent, and without preload, which is effectively
// irreversible and not a default anyone can consent to on an operator's behalf.
//
// Zero omits the header. HSTS is sticky, and unsecuring a site cannot reach
// into browsers that already cached it, so an operator who does not want that
// commitment needs a way to decline it.
func hstsHeaderFor(maxAge int) string {
	if maxAge <= 0 {
		return ""
	}
	return fmt.Sprintf(`    add_header Strict-Transport-Security "max-age=%d" always;`, maxAge)
}

// hstsMaxAge reads the configured value, defaulting to a year.
func hstsMaxAge() int {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return defaultHSTSMaxAge
	}
	if v := cfg.HSTSMaxAge(); v != nil {
		return *v
	}
	return defaultHSTSMaxAge
}

// staplingBlock returns the OCSP stapling directives, or nothing.
//
// Only when the certificate actually names a responder. Let's Encrypt has
// retired OCSP in favour of CRLs, and its certificates no longer carry a
// responder URL; switching stapling on against one makes nginx warn on every
// reload and staple nothing. Reading the certificate rather than assuming means
// a site on an authority that still publishes OCSP gets stapling, and one that
// does not gets a clean config instead of a warning nobody can act on.
//
// ssl_stapling_verify is on because stapling without it lets an upstream hand
// nginx a response it never checked, which is worse than not stapling.
func staplingBlock(responders []string) string {
	if len(responders) == 0 {
		return ""
	}
	return `    ssl_stapling on;
    ssl_stapling_verify on;
    ssl_trusted_certificate /etc/nginx/certs/{{.CertDomain}}.crt;
`
}

// certResponders reads the OCSP responder URLs out of a site's leaf certificate.
// A missing or unreadable certificate reports none, so the vhost is generated
// without stapling and picks it up on the next regeneration once the
// certificate is there.
func certResponders(certDomain string) []string {
	path := filepath.Join(config.CertsDir(), "sites", certDomain+".crt")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	return leaf.OCSPServer
}

// TLSBlock is the whole TLS configuration for one secured vhost: the protocol
// and cipher defaults, the HSTS header, and stapling where the certificate
// supports it.
func tlsBlockFor(certDomain string) string {
	var b strings.Builder
	b.WriteString(tlsBaseBlock)
	if stapling := staplingBlock(certResponders(certDomain)); stapling != "" {
		b.WriteString(strings.ReplaceAll(stapling, "{{.CertDomain}}", certDomain))
	}
	if hsts := hstsHeaderFor(hstsMaxAge()); hsts != "" {
		b.WriteString(hsts + "\n")
	}
	return b.String()
}
