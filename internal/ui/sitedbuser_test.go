package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
)

// dbUserSite registers one Laravel site with an env file, on the install's
// default connection.
func dbUserSite(t *testing.T) *config.Site {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "acme")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	env := "DB_CONNECTION=mysql\nDB_HOST=servlo-mysql\nDB_DATABASE=acme\nDB_USERNAME=root\nDB_PASSWORD=adminsecret\n"
	if err := os.WriteFile(filepath.Join(path, ".env"), []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	site := config.Site{Name: "acme", Domains: []string{"acme.example"}, Path: path, Framework: "laravel"}
	if err := config.SaveSites(&config.SiteRegistry{Sites: []config.Site{site}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	return &loaded.Sites[0]
}

func dbUserGet(t *testing.T, domain string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites/"+domain+"/db-user", nil)
	if !dbUserRoute(rec, req, domain, []string{"db-user"}) {
		t.Fatal("the route did not claim the request")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return body
}

// A site that has not been given an account yet says so, and names the
// administrator it is falling back to rather than pretending it has one.
func TestDBUserStatus_ReportsTheFallbackToTheAdministrator(t *testing.T) {
	site := dbUserSite(t)

	body := dbUserGet(t, site.PrimaryDomain())

	if body["own_account"] != false {
		t.Errorf("own_account = %v, want false for a site that has no account yet", body["own_account"])
	}
	if body["user"] != "root" {
		t.Errorf("user = %v, want the administrator it is actually using", body["user"])
	}
	if body["location"] != "servlo-mysql" {
		t.Errorf("location = %v", body["location"])
	}
}

// Once it has one, the card shows the account and the keys a rotation would
// rewrite, so the operator knows what the button touches before pressing it.
func TestDBUserStatus_ShowsTheAccountAndTheKeysARotationWrites(t *testing.T) {
	site := dbUserSite(t)
	if err := dbconn.RecordSiteUser("mysql", "acme", "acme", "sitepasswordsitepasswordsit"); err != nil {
		t.Fatal(err)
	}

	body := dbUserGet(t, site.PrimaryDomain())

	if body["own_account"] != true || body["user"] != "acme" {
		t.Errorf("status = %v, want the site's own account", body)
	}
	keys, _ := json.Marshal(body["env_keys"])
	for _, want := range []string{"DB_USERNAME", "DB_PASSWORD"} {
		if !strings.Contains(string(keys), want) {
			t.Errorf("env_keys = %s, want %s among them", keys, want)
		}
	}
}

// Nothing about the account's password reaches the browser. It is in the site's
// env file, which is where the application reads it from.
func TestDBUserStatus_NeverCarriesAPassword(t *testing.T) {
	dbUserSite(t)
	if err := dbconn.RecordSiteUser("mysql", "acme", "acme", "sitepasswordsitepasswordsit"); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.example/db-user", nil)
	dbUserRoute(rec, req, "acme.example", []string{"db-user"})

	if strings.Contains(rec.Body.String(), "sitepasswordsitepasswordsit") {
		t.Errorf("the response carries the site's password:\n%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password\"") {
		t.Errorf("the response has a password field at all:\n%s", rec.Body.String())
	}
}

// The route answers what it declares and nothing else: a GET on rotate, or any
// other verb, is not a state change that slipped past the method check.
func TestDBUserRoute_RejectsWhatItDoesNotServe(t *testing.T) {
	site := dbUserSite(t)
	domain := site.PrimaryDomain()

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "rotate"},
		{http.MethodPost, ""},
		{http.MethodDelete, ""},
	} {
		rest := []string{"db-user"}
		if tc.path != "" {
			rest = append(rest, tc.path)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, "/api/sites/"+domain+"/db-user/"+tc.path, nil)
		if !dbUserRoute(rec, req, domain, rest) {
			t.Fatalf("%s %s: the route must claim its own prefix", tc.method, tc.path)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}

// The site tree is reached through the domain in the path, and a domain nobody
// registered must not resolve to one.
func TestDBUserRoute_UnknownSiteIsNotFound(t *testing.T) {
	dbUserSite(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites/nope.example/db-user", nil)
	dbUserRoute(rec, req, "nope.example", []string{"db-user"})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
