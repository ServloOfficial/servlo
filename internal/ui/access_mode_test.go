package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The panel used to ask the server whether nginx was bound past loopback, and
// hid half of itself when the answer was no. There is no such answer any more,
// so the field is pinned true and a panel from an older build keeps working
// rather than telling the operator their sites are unreachable.
func TestAccessModeAlwaysReportsTheSitesAsServed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/access-mode", nil)
	rec := httptest.NewRecorder()
	handleAccessMode(rec, req)

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["sites_served"] != true {
		t.Errorf("response = %+v, want the sites reported as served", body)
	}
	// Where the request came from is not something the panel reports, because
	// nothing is decided by it.
	if _, present := body["local_control"]; present {
		t.Errorf("response still carries local_control: %+v", body)
	}
}
