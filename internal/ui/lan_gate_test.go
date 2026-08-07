package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
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

func TestFullAccessRefusedWhileLoopbackOnly(t *testing.T) {
	setupConfigDirRaw(t, "alice", "s3cret", false)

	rec := httptest.NewRecorder()
	handleRemoteControl(rec, localPost("/api/remote-control", `{"action":"full-access","enabled":true}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	cfg, _ := config.LoadGlobal()
	if cfg != nil && cfg.UI.RemoteFullAccess {
		t.Fatal("refused request still persisted ui.remote_full_access")
	}
}

func TestFullAccessOffAllowedWhileLoopbackOnly(t *testing.T) {
	setupConfigDirRaw(t, "alice", "s3cret", false)

	rec := httptest.NewRecorder()
	handleRemoteControl(rec, localPost("/api/remote-control", `{"action":"full-access","enabled":false}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}
