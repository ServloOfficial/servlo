package cli

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
)

// uiClientDial reports the transport the CLI uses to reach the servlo-panel
// daemon. It's a var so tests can point it at a fake listener.
var uiClientDial = func() (network, addr string) {
	return config.UIClientNetwork(), config.UIClientAddr()
}

// unixHTTPClient talks to servlo-panel over the transport uiClientDial resolves.
func unixHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				network, addr := uiClientDial()
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, addr)
			},
		},
	}
}

func getUnix(path string) ([]byte, int, error) {
	req, _ := http.NewRequest("GET", "http://servlo"+path, nil)
	resp, err := unixHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

func postUnix(path string, body []byte) ([]byte, int, error) {
	req, _ := http.NewRequest("POST", "http://servlo"+path, strings.NewReader(string(body)))
	resp, err := unixHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	return got, resp.StatusCode, err
}

// NotifyPanel tells a running servlo-panel that units changed under it, so an
// open dashboard updates rather than waiting for its next poll.
//
// Over the socket, like everything else a CLI process says to the panel. It
// used to POST to http://127.0.0.1:7073, which stopped being an address the
// panel answers the day it started speaking TLS: the sorting listener sees
// plain HTTP, redirects to https, and the redirect Go follows on its own lands
// on a certificate the panel signed itself, so every notification since has
// ended in a failed verification nothing surfaced. The socket has no such
// problem and needs no certificate to trust.
//
// Best effort by design. A panel that is not running is the ordinary state of a
// CLI-only install, and a command that failed because nobody was watching the
// dashboard would be worse than a dashboard that updates a poll later.
func NotifyPanel() {
	_, _, _ = postUnix("/api/internal/notify", nil)
}
