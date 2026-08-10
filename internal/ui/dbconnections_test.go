package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
)

func connectionsPost(t *testing.T, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/db-connections", strings.NewReader(body))
	req.RemoteAddr = "203.0.113.10:54321"
	rec := httptest.NewRecorder()
	handleDBConnections(rec, req)

	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// A managed database is added from the panel, which is the whole point of the
// panel: an operator who has just clicked through DigitalOcean's console has
// the host and the credentials in front of them.
func TestDBConnections_AddsAManagedDatabase(t *testing.T) {
	setupConfigDir(t, "", "")
	stubConnectionTest(t, nil)

	got := connectionsPost(t, `{"action":"add","name":"managed","engine":"postgres",
		"host":"db.example.net","port":25060,"user":"doadmin","password":"s3cret","tls_mode":"require"}`)
	if got["error"] != nil {
		t.Fatalf("add failed: %v", got["error"])
	}

	c, err := dbconn.Named("managed")
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "db.example.net" || c.Port != 25060 || c.Password != "s3cret" {
		t.Errorf("stored connection = %+v", c)
	}
}

// The password goes in and never comes back. A panel that renders a credential
// is a panel that renders it in a screen share, and the operator has no reason
// to read it back: they typed it.
func TestDBConnections_NeverRendersThePassword(t *testing.T) {
	setupConfigDir(t, "", "")
	stubConnectionTest(t, nil)

	if got := connectionsPost(t, `{"action":"add","name":"managed","engine":"mysql",
		"host":"db.example.net","user":"admin","password":"hunter2"}`); got["error"] != nil {
		t.Fatalf("add failed: %v", got["error"])
	}

	req := httptest.NewRequest(http.MethodGet, "/api/db-connections", nil)
	rec := httptest.NewRecorder()
	handleDBConnections(rec, req)

	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("the response carries the password:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "db.example.net") {
		t.Errorf("the response does not carry the connection at all:\n%s", rec.Body.String())
	}
}

// Assigning is where a site's next env write goes, and nothing else. No data
// moves, because a dropdown that migrated a production database on change is
// the most expensive possible misreading of a click.
func TestDBConnections_AssignsASiteWithoutMovingData(t *testing.T) {
	setupConfigDir(t, "", "")
	stubConnectionTest(t, nil)
	registerSite(t, "shop", "shop.example")

	if got := connectionsPost(t, `{"action":"add","name":"managed","engine":"mysql",
		"host":"db.example.net","user":"admin","password":"pw"}`); got["error"] != nil {
		t.Fatalf("add failed: %v", got["error"])
	}
	if got := connectionsPost(t, `{"action":"assign","domain":"shop.example","connection":"managed"}`); got["error"] != nil {
		t.Fatalf("assign failed: %v", got["error"])
	}

	site, err := config.FindSiteByDomain("shop.example")
	if err != nil {
		t.Fatal(err)
	}
	if site.Database != "managed" {
		t.Errorf("site database = %q, want managed", site.Database)
	}
}

// A connection with sites on it is not removed out from under them: their env
// still points at that database, and the name they carry would resolve to
// nothing.
func TestDBConnections_RefusesToRemoveOneInUse(t *testing.T) {
	setupConfigDir(t, "", "")
	stubConnectionTest(t, nil)
	registerSite(t, "shop", "shop.example")

	connectionsPost(t, `{"action":"add","name":"managed","engine":"mysql","host":"db.example.net","user":"admin","password":"pw"}`)
	connectionsPost(t, `{"action":"assign","domain":"shop.example","connection":"managed"}`)

	got := connectionsPost(t, `{"action":"remove","name":"managed"}`)

	message, _ := got["error"].(string)
	if message == "" {
		t.Fatal("a connection with a site on it was removed")
	}
	if !strings.Contains(message, "shop.example") {
		t.Errorf("error = %q, does not say which site is in the way", message)
	}
	if _, err := dbconn.Named("managed"); err != nil {
		t.Errorf("the connection was removed anyway: %v", err)
	}
}

// A half-configured connection is refused here rather than at the first deploy,
// by which time the site exists and its env is written against a database
// nothing can reach.
func TestDBConnections_RefusesAnIncompleteConnection(t *testing.T) {
	setupConfigDir(t, "", "")

	got := connectionsPost(t, `{"action":"add","name":"managed","engine":"mysql","host":"db.example.net"}`)

	if got["error"] == nil {
		t.Fatal("a connection with no user or password was accepted")
	}
}
