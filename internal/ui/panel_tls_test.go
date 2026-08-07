package ui

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
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
