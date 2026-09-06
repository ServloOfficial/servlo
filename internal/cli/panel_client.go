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
