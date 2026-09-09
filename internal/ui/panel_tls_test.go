package ui

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
)

// The panel is where the operator types a password, and on a server the wire
// between them is the internet. It serves TLS, self-signed at first, and the
// listener refuses to be a plain HTTP server no matter how it is reached.
func TestPanelListener_ServesTLS(t *testing.T) {
	ln, addr := panelTestListener(t)
	defer ln.Close()

	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // the certificate is self-signed by design
	if err != nil {
		t.Fatalf("TLS handshake against the panel: %v", err)
	}
	defer conn.Close()
	if len(conn.ConnectionState().PeerCertificates) == 0 {
		t.Fatal("the panel presented no certificate")
	}
}

// Typing http:// where https:// belongs is the single most likely thing an
// operator does, and a TLS listener answering it raw produces a browser error
// that says nothing useful. Answering the redirect costs one byte of lookahead.
func TestPanelListener_RedirectsPlainHTTP(t *testing.T) {
	ln, addr := panelTestListener(t)
	defer ln.Close()

	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get("http://" + addr + "/some/path?q=1")
	if err != nil {
		t.Fatalf("plain HTTP request: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusPermanentRedirect)
	}
	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, "https://") {
		t.Errorf("Location = %q, want an https URL", location)
	}
	// The path and query have to survive, or a bookmarked deep link lands on
	// the dashboard root and the operator concludes the link is broken.
	if !strings.HasSuffix(location, "/some/path?q=1") {
		t.Errorf("Location = %q, want the original path and query preserved", location)
	}
}

// A redirect is a hint, not a channel: whatever was in the plain request was
// already in the clear, so the response must not carry anything from the panel.
func TestPanelListener_PlainHTTPRedirectCarriesNoContent(t *testing.T) {
	ln, addr := panelTestListener(t)
	defer ln.Close()

	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get("http://" + addr + "/api/sites")
	if err != nil {
		t.Fatalf("plain HTTP request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if len(body) > 0 && strings.Contains(string(body), "{") {
		t.Errorf("the plain-HTTP responder returned what looks like panel data: %q", body)
	}
	if len(resp.Cookies()) != 0 {
		t.Error("the plain-HTTP responder set a cookie, which would send it in the clear next time")
	}
}

// panelTestListener starts the panel's TLS-or-redirect listener on a free port
// with a self-signed certificate and a handler that answers anything.
func panelTestListener(t *testing.T) (net.Listener, string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	go func() {
		_ = servePanelTLS(raw, handler)
	}()
	return raw, raw.Addr().String()
}

// writeStandInCertificate puts a parseable certificate and key for domain where
// the ACME flow would have written them, so the handshake path has a real one
// to prefer. Its common name is what tells it apart from the self-signed panel
// certificate in the assertions below.
func writeStandInCertificate(t *testing.T, domain string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "a real authority would have signed this"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{domain},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := certs.SitePaths(domain)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func panelCertHome(t *testing.T, domain string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := &config.GlobalConfig{}
	cfg.UI.Domain = domain
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}
}

func leafCommonName(t *testing.T, cert *tls.Certificate) string {
	t.Helper()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parsing the certificate the panel would present: %v", err)
	}
	return leaf.Subject.CommonName
}

// Port 7073 is the way back in when DNS is wrong or nginx is down, and it is
// reached by address, which sends no SNI. Answering that with the certificate
// for the panel's domain would make the emergency route a name mismatch.
func TestPanelCertificateFor_ByAddressStaysOnTheSelfSignedCertificate(t *testing.T) {
	panelCertHome(t, "panel.example.com")
	writeStandInCertificate(t, "panel.example.com")

	cert, err := panelCertificateFor(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("panelCertificateFor: %v", err)
	}
	if cn := leafCommonName(t, cert); cn != "Servlo panel" {
		t.Errorf("a connection by address was answered with %q", cn)
	}
}

// Asked for by name, the real certificate is the point of having issued one.
func TestPanelCertificateFor_ByNameServesTheRealCertificate(t *testing.T) {
	panelCertHome(t, "panel.example.com")
	writeStandInCertificate(t, "panel.example.com")

	cert, err := panelCertificateFor(&tls.ClientHelloInfo{ServerName: "panel.example.com"})
	if err != nil {
		t.Fatalf("panelCertificateFor: %v", err)
	}
	if cn := leafCommonName(t, cert); cn == "Servlo panel" {
		t.Error("a connection by name got the self-signed certificate even though a real one exists")
	}
}

// Before `servlo panel domain secure` there is no real certificate, and the
// self-signed one covers the domain as well as the addresses, so reaching the
// panel by name in the meantime is one warning rather than two.
func TestPanelCertificateFor_ByNameFallsBackBeforeThereIsARealOne(t *testing.T) {
	panelCertHome(t, "panel.example.com")

	cert, err := panelCertificateFor(&tls.ClientHelloInfo{ServerName: "panel.example.com"})
	if err != nil {
		t.Fatalf("panelCertificateFor: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "Servlo panel" {
		t.Errorf("presented %q with no certificate issued", leaf.Subject.CommonName)
	}
	if err := leaf.VerifyHostname("panel.example.com"); err != nil {
		t.Errorf("the self-signed certificate does not cover the panel's domain: %v", err)
	}
}
