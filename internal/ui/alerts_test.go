package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/alerts"
)

func alertsEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func getAlerts(t *testing.T) AlertsResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	handleAlerts(rec, httptest.NewRequest(http.MethodGet, "/api/alerts", nil))

	var got AlertsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("the panel got something that is not JSON: %s", rec.Body.String())
	}
	return got
}

// The kind is the server's vocabulary. Sending the heading with it keeps the
// panel from carrying a second copy of that list that can drift out of step
// with the one the emails use.
func TestHandleAlerts_SendsTheHeadingWithTheKind(t *testing.T) {
	alertsEnv(t)
	if err := alerts.Raise(alerts.Alert{
		Kind: alerts.KindSiteDown, Site: "acme", Message: "the site answered 502",
	}); err != nil {
		t.Fatal(err)
	}

	got := getAlerts(t)
	if len(got.Alerts) != 1 {
		t.Fatalf("the panel got %d alerts", len(got.Alerts))
	}
	if got.Alerts[0].Title == "" || got.Alerts[0].Title == got.Alerts[0].Kind {
		t.Errorf("the alert's heading is %q, which is the kind rather than something to read", got.Alerts[0].Title)
	}
	if got.Alerts[0].Site != "acme" || !strings.Contains(got.Alerts[0].Message, "502") {
		t.Errorf("the alert lost its detail: %+v", got.Alerts[0])
	}
}

// A server with nothing wrong sends an empty list, not null. The panel renders
// a list, and null is how a card ends up showing "undefined".
func TestHandleAlerts_EmptyIsAListNotNull(t *testing.T) {
	alertsEnv(t)
	rec := httptest.NewRecorder()
	handleAlerts(rec, httptest.NewRequest(http.MethodGet, "/api/alerts", nil))

	if !strings.Contains(rec.Body.String(), `"alerts":[]`) {
		t.Errorf("an empty list serialised as %s", rec.Body.String())
	}
}

// Dismissing takes it off the list and hands the new list straight back, so the
// panel does not need a second request to redraw.
func TestHandleAlerts_DismissRemovesItAndReturnsWhatIsLeft(t *testing.T) {
	alertsEnv(t)
	_ = alerts.Raise(alerts.Alert{Kind: alerts.KindSiteDown, Site: "acme", Message: "502"})
	_ = alerts.Raise(alerts.Alert{Kind: alerts.KindDiskFilling, Message: "94% full"})

	body := strings.NewReader(`{"kind":"site_down","site":"acme"}`)
	rec := httptest.NewRecorder()
	handleAlerts(rec, httptest.NewRequest(http.MethodPost, "/api/alerts", body))

	var got AlertsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "" {
		t.Fatalf("dismissing failed: %s", got.Error)
	}
	if len(got.Alerts) != 1 || got.Alerts[0].Kind != alerts.KindDiskFilling {
		t.Errorf("after dismissing one alert the list is %+v", got.Alerts)
	}
}

// A dismissal that names nothing is refused rather than clearing whichever
// alert happens to have an empty site, which is the server-wide ones.
func TestHandleAlerts_DismissWithoutAKindIsRefused(t *testing.T) {
	alertsEnv(t)
	_ = alerts.Raise(alerts.Alert{Kind: alerts.KindDiskFilling, Message: "94% full"})

	rec := httptest.NewRecorder()
	handleAlerts(rec, httptest.NewRequest(http.MethodPost, "/api/alerts", strings.NewReader(`{}`)))

	var got AlertsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Error == "" {
		t.Error("a dismissal that named no alert was accepted")
	}
	if left := getAlerts(t); len(left.Alerts) != 1 {
		t.Errorf("it cleared something anyway: %+v", left.Alerts)
	}
}
