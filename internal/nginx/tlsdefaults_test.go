package nginx

import (
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func securedVhost(t *testing.T) (body, tmp string) {
	t.Helper()
	tmp = t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	site := config.Site{Name: "myapp", Domains: []string{"example.com"}, Path: t.TempDir(), PHPVersion: "8.4", Secured: true}
	if err := GenerateSSLVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateSSLVhost: %v", err)
	}
	return vhostBody(t, tmp, "example.com-ssl.conf"), tmp
}

// Without an explicit ssl_protocols, a vhost inherits whatever the nginx image
// happens to default to, which has included TLS 1.0 and 1.1 within the life of
// images still in use. A panel that issues a real certificate and then serves
// it over a protocol deprecated in 2021 is not doing its job.
func TestSecuredVhost_RefusesTLSBelow12(t *testing.T) {
	body, _ := securedVhost(t)

	if !strings.Contains(body, "ssl_protocols") {
		t.Fatalf("the vhost sets no ssl_protocols, so it inherits the image's default:\n%s", body)
	}
	protocols := lineContaining(body, "ssl_protocols")
	for _, dead := range []string{"TLSv1 ", "TLSv1;", "TLSv1.1", "SSLv2", "SSLv3"} {
		if strings.Contains(protocols, dead) {
			t.Errorf("ssl_protocols still offers %s: %s", dead, protocols)
		}
	}
	for _, want := range []string{"TLSv1.2", "TLSv1.3"} {
		if !strings.Contains(protocols, want) {
			t.Errorf("ssl_protocols does not offer %s: %s", want, protocols)
		}
	}
}

func TestSecuredVhost_SetsModernCiphers(t *testing.T) {
	body, _ := securedVhost(t)
	ciphers := lineContaining(body, "ssl_ciphers")
	if ciphers == "" {
		t.Fatalf("the vhost sets no ssl_ciphers:\n%s", body)
	}
	// The suites that carry the known-broken constructions. None of them should
	// survive into a cipher list servlo generates.
	for _, dead := range []string{"RC4", "3DES", "DES-CBC", "MD5", "NULL", "EXPORT"} {
		if strings.Contains(ciphers, dead) {
			t.Errorf("the cipher list includes %s: %s", dead, ciphers)
		}
	}
	if !strings.Contains(ciphers, "ECDHE") {
		t.Errorf("the cipher list offers no forward secrecy: %s", ciphers)
	}
}

// Letting the client pick is the current guidance rather than an oversight. A
// phone without AES hardware is faster and no less safe on ChaCha20, and it is
// the only party that knows which it is.
func TestSecuredVhost_LetsTheClientChooseTheCipher(t *testing.T) {
	body, _ := securedVhost(t)
	if line := lineContaining(body, "ssl_prefer_server_ciphers"); !strings.Contains(line, "off") {
		t.Errorf("ssl_prefer_server_ciphers is not off: %q", line)
	}
}

// Session tickets off, because nginx reuses one ticket key for the life of the
// process. Anyone who later obtains it can decrypt every session recorded since
// it was created, which is exactly the forward secrecy the cipher list buys.
func TestSecuredVhost_DisablesSessionTickets(t *testing.T) {
	body, _ := securedVhost(t)
	if line := lineContaining(body, "ssl_session_tickets"); !strings.Contains(line, "off") {
		t.Errorf("ssl_session_tickets is not off: %q", line)
	}
}

// ── HSTS max-age ──────────────────────────────────────────────────────────────

func TestHSTSHeader_UsesTheConfiguredMaxAge(t *testing.T) {
	if got := hstsHeaderFor(60); !strings.Contains(got, "max-age=60") {
		t.Errorf("HSTS header = %q, want the configured max-age", got)
	}
	if got := hstsHeaderFor(defaultHSTSMaxAge); !strings.Contains(got, "max-age=31536000") {
		t.Errorf("the default HSTS header = %q, want a year", got)
	}
}

// HSTS is sticky: a browser that has seen it refuses plain http for that host
// until the max-age runs out, and unsecuring the site cannot reach into
// browsers that cached it. An operator who does not want that commitment needs
// a way to decline it, and zero is the natural way to say so.
func TestHSTSHeader_ZeroMaxAgeOmitsTheHeaderEntirely(t *testing.T) {
	if got := hstsHeaderFor(0); got != "" {
		t.Errorf("max-age 0 still emitted a header: %q", got)
	}
}

func TestSecuredVhost_OmitsHSTSWhenTurnedOff(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	off := 0
	cfg.Certs.HSTSMaxAge = &off
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}

	site := config.Site{Name: "myapp", Domains: []string{"example.com"}, Path: t.TempDir(), PHPVersion: "8.4", Secured: true}
	if err := GenerateSSLVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateSSLVhost: %v", err)
	}
	if body := vhostBody(t, tmp, "example.com-ssl.conf"); strings.Contains(body, "Strict-Transport-Security") {
		t.Errorf("HSTS was turned off but the header is still sent:\n%s", body)
	}
}

// ── OCSP stapling ─────────────────────────────────────────────────────────────

// Stapling is only emitted when the certificate names a responder to staple
// from. Let's Encrypt has retired OCSP in favour of CRLs, and turning stapling
// on against a certificate with no responder URL makes nginx warn on every
// reload while doing nothing at all.
func TestStaplingBlock_OnlyWhenTheCertificateNamesAResponder(t *testing.T) {
	if got := staplingBlock(nil); got != "" {
		t.Errorf("stapling was emitted for a certificate with no responder: %q", got)
	}
	got := staplingBlock([]string{"http://ocsp.example.org"})
	if !strings.Contains(got, "ssl_stapling on") {
		t.Errorf("stapling was not emitted for a certificate that names one: %q", got)
	}
	// Stapling without verification lets an upstream hand nginx an unsigned
	// response, which is worse than not stapling.
	if !strings.Contains(got, "ssl_stapling_verify on") {
		t.Errorf("stapling is on without verification: %q", got)
	}
	if !strings.Contains(got, "ssl_trusted_certificate") {
		t.Errorf("stapling has no trusted chain to verify against: %q", got)
	}
}

func lineContaining(body, needle string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
