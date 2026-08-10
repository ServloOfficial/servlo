package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/authz"
	"github.com/realrashid/servlo/internal/config"
)

func stagingEnv(t *testing.T) (live, stage config.Site) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))

	live = config.Site{Name: "acme", Domains: []string{"acme.example"}, Path: filepath.Join(root, "acme")}
	stage = config.Site{
		Name: "staging-acme", Domains: []string{"staging.acme.example"},
		Path:    filepath.Join(root, "staging"),
		Staging: &config.SiteStaging{Origin: "acme", User: "staging", Hash: "$2a$10$abc"},
	}
	for _, s := range []config.Site{live, stage} {
		if err := config.AddSite(s); err != nil {
			t.Fatal(err)
		}
	}
	return live, stage
}

func getStaging(t *testing.T, domain string) SiteStagingResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites/"+domain+"/staging", nil)
	if !stagingRoute(rec, req, domain, []string{"staging"}) {
		t.Fatal("the route did not claim the request")
	}
	var got SiteStagingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("the panel got something that is not JSON: %s", rec.Body.String())
	}
	return got
}

// A staging site shows what it copies from, by domain: the internal name is
// servlo's and means nothing to somebody reading a panel full of domains.
func TestStagingRoute_ShowsTheOriginAsADomain(t *testing.T) {
	stagingEnv(t)

	got := getStaging(t, "staging.acme.example")
	if !got.Staging {
		t.Fatal("a staging site was not reported as one")
	}
	if got.OriginDomain != "acme.example" {
		t.Errorf("the origin reads as %q, want the live site's address", got.OriginDomain)
	}
	if !got.OriginExists {
		t.Error("the live site exists and the panel says it does not")
	}
}

// The other half of the same relationship: a live site says which copies exist,
// which is what somebody about to deploy actually wants to know.
func TestStagingRoute_ALiveSiteListsItsCopies(t *testing.T) {
	stagingEnv(t)

	got := getStaging(t, "acme.example")
	if got.Staging {
		t.Error("a live site was reported as a staging one")
	}
	if len(got.Copies) != 1 || got.Copies[0] != "staging.acme.example" {
		t.Errorf("the live site lists %v as its copies", got.Copies)
	}
}

// A live site with no copies sends an empty list, not null, or the card renders
// "undefined" on every site on the server.
func TestStagingRoute_EmptyIsAListNotNull(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	if err := config.AddSite(config.Site{Name: "solo", Domains: []string{"solo.example"}, Path: root}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites/solo.example/staging", nil)
	stagingRoute(rec, req, "solo.example", []string{"staging"})

	if !strings.Contains(rec.Body.String(), `"copies":[]`) {
		t.Errorf("an empty list serialised as %s", rec.Body.String())
	}
}

// A refresh copies the live site's database across, so a Developer given only
// the staging site must not be able to pull production data onto a site they
// control. Whether they should see that data is not this site's assignment to
// answer.
func TestStagingRoute_RefreshNeedsAnAdmin(t *testing.T) {
	_, stage := stagingEnv(t)

	body := strings.NewReader(`{"action":"refresh"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/"+stage.PrimaryDomain()+"/staging", body)
	req = req.WithContext(authz.WithScope(req.Context(), authz.Scope{Role: authz.RoleDeveloper, Sites: []string{stage.Name}}))
	rec := httptest.NewRecorder()
	stagingRoute(rec, req, stage.PrimaryDomain(), []string{"staging"})

	var got SiteStagingActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.OK {
		t.Fatal("a developer refreshed a staging site from live")
	}
	if !strings.Contains(got.Error, "admin") {
		t.Errorf("the refusal does not say what is needed: %q", got.Error)
	}
}

// Acting on a site that is not staging is refused, wherever the request came
// from. The card hides the buttons; the API does not rely on that.
func TestStagingRoute_RefusesAnActionOnASiteThatIsNotStaging(t *testing.T) {
	live, _ := stagingEnv(t)

	body := strings.NewReader(`{"action":"refresh"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/"+live.PrimaryDomain()+"/staging", body)
	rec := httptest.NewRecorder()
	stagingRoute(rec, req, live.PrimaryDomain(), []string{"staging"})

	var got SiteStagingActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.OK || got.Error == "" {
		t.Errorf("an action on a live site returned %+v", got)
	}
}

func TestStagingRoute_RefusesAnUnknownAction(t *testing.T) {
	_, stage := stagingEnv(t)

	body := strings.NewReader(`{"action":"promote-to-live"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sites/"+stage.PrimaryDomain()+"/staging", body)
	req = req.WithContext(adminScope(req.Context()))
	rec := httptest.NewRecorder()
	stagingRoute(rec, req, stage.PrimaryDomain(), []string{"staging"})

	var got SiteStagingActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.OK || got.Error == "" {
		t.Errorf("an unknown action returned %+v", got)
	}
}

func adminScope(ctx context.Context) context.Context {
	return authz.WithScope(ctx, authz.Scope{Role: authz.RoleAdmin})
}
