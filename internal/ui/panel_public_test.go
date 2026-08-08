package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The panel's unix socket is treated as local control, because until now the
// only thing proxying into it was the servlo.localhost vhost, and .localhost
// resolves to the visitor's own loopback (RFC 6761) so no remote browser can
// reach it.
//
// A panel domain breaks that assumption: it is a real name on the public
// internet proxying into the same socket. Without a marker, attaching one would
// have the CSRF gate trust every request that arrived over it. The vhost sets
// the marker with proxy_set_header, which overwrites whatever the client sent,
// so it cannot be stripped from outside.
func TestPublicPanelRequest_IsNotLocalControl(t *testing.T) {
	req := unixSocketRequest(http.MethodPost, "/api/servlo/stop")
	req.Header.Set(publicPanelHeader, "1")

	if isLocalControlRequest(req) {
		t.Error("a request that arrived over the public panel domain was granted local control")
	}
	if isLoopbackRequest(req) {
		t.Error("a request that arrived over the public panel domain was treated as loopback")
	}
	if passesCSRF(req) {
		t.Error("a request that arrived over the public panel domain skipped the cross-origin check")
	}
}

// The servlo.localhost vhost keeps working, or the local dashboard loses the
// only path it has.
func TestLocalPanelRequest_KeepsLocalControl(t *testing.T) {
	req := unixSocketRequest(http.MethodGet, "/api/sites")

	if !isLocalControlRequest(req) {
		t.Error("the local dashboard lost local control")
	}
	if !isLoopbackRequest(req) {
		t.Error("the local dashboard is no longer treated as loopback")
	}
}

// Setting the header only ever reduces what a request may do, so a client
// sending it straight to the port is denying itself and nobody else. The
// direction matters: a header that granted anything would be one an attacker
// could forge.
func TestPublicPanelHeader_OnlyEverRemovesTrust(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/servlo/stop", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	if !isLocalControlRequest(req) {
		t.Fatal("a plain loopback request should have local control")
	}

	req.Header.Set(publicPanelHeader, "1")
	if isLocalControlRequest(req) {
		t.Error("the marker did not remove local control from a loopback request")
	}
}

// unixSocketRequest builds a request shaped like one arriving over the panel's
// unix socket listener.
func unixSocketRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "@"
	return req.WithContext(context.WithValue(req.Context(), ctxKeyUnixSocket{}, true))
}
