package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func localPost(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "localhost:7073"
	req.Header.Set("X-Servlo-CSRF", "1")
	return req
}

// Databases and caches are loopback-only always, so the route that used to
// publish them has no such action left. An old dashboard, or anyone probing the
// API with the name it used to answer to, is refused rather than served.
func TestLANStatusHasNoActionThatPublishesAService(t *testing.T) {
	setupConfigDirRaw(t, "", "", false)

	for _, action := range []string{"services_on", "services_off"} {
		rec := httptest.NewRecorder()
		handleLANStatus(rec, localPost("/api/lan/status", `{"action":"`+action+`"}`))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d (%s)", action, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	}
}

// The host-action opt-in is gone with the gate it widened, and an old client
// still asking for it is answered rather than obeyed.
func TestRemoteControlHasNoFullAccessAction(t *testing.T) {
	setupConfigDirRaw(t, "alice", "s3cret", false)

	rec := httptest.NewRecorder()
	handleRemoteControl(rec, localPost("/api/remote-control", `{"action":"full-access","enabled":true}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
