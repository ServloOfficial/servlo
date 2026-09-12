package cli

import (
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// The panel speaks TLS on its TCP port and answers plain HTTP there with a
// redirect to itself over https, which Go's client follows into a certificate
// the panel signed itself. So a notification aimed at that port cannot arrive,
// and internal/config says as much beside the socket address: the socket is the
// transport a CLI process uses, rather than the TCP loopback the dashboard
// does.
func TestNotifyPanel_GoesOverTheSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "panel.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	got := make(chan string, 1)
	go func() {
		_ = (&http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case got <- r.Method + " " + r.URL.Path:
			default:
			}
			w.WriteHeader(http.StatusOK)
		})}).Serve(ln)
	}()

	old := uiClientDial
	uiClientDial = func() (string, string) { return "unix", sock }
	t.Cleanup(func() { uiClientDial = old })

	NotifyPanel()

	select {
	case saw := <-got:
		if saw != "POST /api/internal/notify" {
			t.Errorf("the panel saw %q", saw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the panel was never told anything")
	}
}

// A CLI-only install has no panel running, and a command must not fail because
// nobody was watching the dashboard.
func TestNotifyPanel_SaysNothingWhenNobodyIsListening(t *testing.T) {
	old := uiClientDial
	uiClientDial = func() (string, string) { return "unix", filepath.Join(t.TempDir(), "absent.sock") }
	t.Cleanup(func() { uiClientDial = old })

	NotifyPanel()
}
