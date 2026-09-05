package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/appinstall"
	"github.com/ServloOfficial/servlo/internal/config"
)

// The form has to know two things about an app before anybody fills it in:
// whether a database is coming, and whether the install ends with an account or
// with a page the operator has to finish. Both are properties of the definition
// and neither is guessable from the name.
func TestHandleApps_SaysWhatEachAppWillAndWillNotDo(t *testing.T) {
	rec := httptest.NewRecorder()
	handleApps(rec, httptest.NewRequest(http.MethodGet, "/api/apps", nil))

	var got struct {
		Apps []AppRow `json:"apps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("reading the response: %v\n%s", err, rec.Body)
	}
	if len(got.Apps) == 0 {
		t.Fatal("the store shipped nothing, so this proves nothing")
	}

	var withSetup, selfSetup, withDB int
	for _, a := range got.Apps {
		if a.Label == "" || a.Version == "" {
			t.Errorf("%s has no label or no version, so the form has nothing to show: %+v", a.Name, a)
		}
		if a.SelfSetup {
			selfSetup++
		} else {
			withSetup++
		}
		if a.NeedsDatabase {
			withDB++
		}
	}
	// Both shapes ship, and a response that collapsed them would have the form
	// promising an admin account for an app that creates none.
	if withSetup == 0 || selfSetup == 0 {
		t.Errorf("every app reported the same setup shape (%d driven, %d self), so the flag is not being read", withSetup, selfSetup)
	}
	if withDB == 0 {
		t.Error("no app reported needing a database, so the flag is not being read")
	}
}

func TestHandleSiteApp_ReturnsTheCredentialsOnce(t *testing.T) {
	defer swapInstall(func(context.Context, appinstall.Options) (appinstall.Installed, error) {
		return appinstall.Installed{
			Site:          config.Site{Name: "blog-acme-com", Domains: []string{"blog.acme.com"}, Path: "/srv/blog"},
			AdminUser:     "admin",
			AdminPassword: "generated",
		}, nil
	})()

	got := postApp(t, `{"app":"example","domain":"blog.acme.com"}`)
	if !got.OK || got.AdminPassword != "generated" {
		t.Fatalf("the panel has nothing to show the operator: %+v", got)
	}
	if got.Domain != "blog.acme.com" || got.Path != "/srv/blog" {
		t.Errorf("the response does not say where the site is: %+v", got)
	}
}

// An app whose own installer servlo cannot drive must say so on the way out.
// A response that looked like every other success would leave an uninstalled
// application on a live domain with nothing on screen about it.
func TestHandleSiteApp_PassesOnWhatIsLeftToDo(t *testing.T) {
	defer swapInstall(func(context.Context, appinstall.Options) (appinstall.Installed, error) {
		return appinstall.Installed{
			Site: config.Site{Name: "shop", Domains: []string{"shop.acme.com"}},
			Note: "Example finishes in its own installer.",
		}, nil
	})()

	got := postApp(t, `{"app":"example","domain":"shop.acme.com"}`)
	if !got.OK {
		t.Fatalf("a completed install was reported as a failure: %+v", got)
	}
	if got.AdminPassword != "" {
		t.Error("a password was reported for an install that created no account")
	}
	if !strings.Contains(got.Note, "own installer") {
		t.Errorf("the note did not reach the panel: %+v", got)
	}
}

func TestHandleSiteApp_ReportsTheRefusalRatherThanASite(t *testing.T) {
	defer swapInstall(func(context.Context, appinstall.Options) (appinstall.Installed, error) {
		return appinstall.Installed{}, errors.New("blog.acme.com is already served by blog")
	})()

	got := postApp(t, `{"app":"example","domain":"blog.acme.com"}`)
	if got.OK || !strings.Contains(got.Error, "already served") {
		t.Fatalf("a refused install did not come back as one: %+v", got)
	}
}

func TestHandleSiteApp_RefusesAnythingButAPost(t *testing.T) {
	rec := httptest.NewRecorder()
	handleSiteApp(rec, httptest.NewRequest(http.MethodGet, "/api/sites/app", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET was answered with %d", rec.Code)
	}
}

func swapInstall(fn func(context.Context, appinstall.Options) (appinstall.Installed, error)) func() {
	old := installApp
	installApp = fn
	return func() { installApp = old }
}

func postApp(t *testing.T, body string) SiteAppResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	handleSiteApp(rec, httptest.NewRequest(http.MethodPost, "/api/sites/app", strings.NewReader(body)))
	var got SiteAppResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("reading the response: %v\n%s", err, rec.Body)
	}
	return got
}
