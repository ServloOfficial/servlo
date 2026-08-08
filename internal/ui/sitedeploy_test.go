package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/deploy"
)

func deployHome(t *testing.T) *config.Site {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	path := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	site := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: path, PHPVersion: "8.4",
	}
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}
	return &site
}

func stubDeploy(t *testing.T, res deploy.Result, err error) *int {
	t.Helper()
	calls := 0
	prev := runDeployFn
	runDeployFn = func(o deploy.Options) (deploy.Result, error) {
		calls++
		if o.Out != nil {
			_, _ = o.Out.Write([]byte("pretend output\n"))
		}
		return res, err
	}
	t.Cleanup(func() { runDeployFn = prev })
	return &calls
}

// doneFrame pulls the terminal `event: done` payload out of an SSE response.
func doneFrame(t *testing.T, body string) map[string]any {
	t.Helper()
	idx := strings.LastIndex(body, "event: done\ndata: ")
	if idx < 0 {
		t.Fatalf("no done frame in the stream:\n%s", body)
	}
	line := body[idx+len("event: done\ndata: "):]
	if nl := strings.Index(line, "\n"); nl >= 0 {
		line = line[:nl]
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatalf("done frame is not JSON: %v\n%s", err, line)
	}
	return payload
}

func postDeploy(t *testing.T, site *config.Site) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/sites/"+site.PrimaryDomain()+"/deploy", nil)
	w := httptest.NewRecorder()
	handleSiteDeploy(w, r, site)
	return w
}

func TestHandleSiteDeploy_StreamsAndReportsWhatItDid(t *testing.T) {
	site := deployHome(t)
	stubDeploy(t, deploy.Result{FromCommit: "aaaa111", ToCommit: "bbbb222", Snapshot: "snap-1"}, nil)

	w := postDeploy(t, site)

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want an event stream", ct)
	}
	if !strings.Contains(w.Body.String(), "pretend output") {
		t.Errorf("the deploy's own output did not reach the stream:\n%s", w.Body.String())
	}
	got := doneFrame(t, w.Body.String())
	if got["ok"] != true {
		t.Errorf("done = %v, want ok", got)
	}
	if got["from"] != "aaaa111" || got["to"] != "bbbb222" {
		t.Errorf("done = %v, does not say which commits it moved between", got)
	}
	if got["snapshot"] != "snap-1" {
		t.Errorf("done = %v, does not name the backup it took", got)
	}
}

// The response has been streaming for minutes by the time a deploy fails, so
// its headers went out long ago. The failure has to arrive in the stream or
// the panel sees a stream that simply stops.
func TestHandleSiteDeploy_ReportsAFailureInTheStream(t *testing.T) {
	site := deployHome(t)
	stubDeploy(t, deploy.Result{FromCommit: "aaaa111", ToCommit: "bbbb222"},
		errors.New("the deploy script failed, and the new code was not made live"))

	w := postDeploy(t, site)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; a stream that already started cannot change its status", w.Code)
	}
	got := doneFrame(t, w.Body.String())
	if got["ok"] != false {
		t.Errorf("done = %v, want a failure", got)
	}
	if !strings.Contains(got["error"].(string), "not made live") {
		t.Errorf("done = %v, does not carry the reason", got)
	}
	// What it managed before failing. A deploy that pulled and then failed its
	// script left the site on code that was never prepared, and that is the
	// single most important thing to know from here.
	if got["to"] != "bbbb222" {
		t.Errorf("done = %v, does not say the pull had already happened", got)
	}
}

// Two deploys at once, or a deploy while a framework command is running, is two
// processes writing the same vendor directory.
func TestHandleSiteDeploy_RefusesWhileTheSiteIsBusy(t *testing.T) {
	site := deployHome(t)
	calls := stubDeploy(t, deploy.Result{}, nil)

	release, _, ok := tryAcquireRun(siteRunLockKey(site), "migrate")
	if !ok {
		t.Fatal("could not take the lock the handler competes for")
	}
	defer release()

	w := postDeploy(t, site)

	if *calls != 0 {
		t.Error("a deploy started while the site was busy")
	}
	if !strings.Contains(w.Body.String(), "migrate") {
		t.Errorf("the refusal does not say what the site is busy with:\n%s", w.Body.String())
	}
}

// And the lock is given back, or the site can never deploy again.
func TestHandleSiteDeploy_ReleasesTheLockAfterwards(t *testing.T) {
	site := deployHome(t)
	stubDeploy(t, deploy.Result{}, errors.New("failed"))

	postDeploy(t, site)

	release, busyWith, ok := tryAcquireRun(siteRunLockKey(site), "second")
	if !ok {
		t.Fatalf("the lock was still held by %q after the deploy finished", busyWith)
	}
	release()
}

func TestHandleSiteDeploy_RefusesOtherMethods(t *testing.T) {
	site := deployHome(t)
	calls := stubDeploy(t, deploy.Result{}, nil)

	r := httptest.NewRequest(http.MethodGet, "/api/sites/shop.example/deploy", nil)
	w := httptest.NewRecorder()
	handleSiteDeploy(w, r, site)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if *calls != 0 {
		t.Error("a GET deployed the site")
	}
}

// The editor needs to know whether saving this script means the next deploy
// takes a database backup, because that is the difference between a deploy
// that can be undone and one that cannot.
func TestHandleSiteDeployScript_SaysWhetherItMigrates(t *testing.T) {
	site := deployHome(t)

	r := httptest.NewRequest(http.MethodGet, "/api/sites/shop.example/deploy-script", nil)
	w := httptest.NewRecorder()
	handleSiteDeployScript(w, r, site)

	var got SiteDeployScriptResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Body == "" {
		t.Error("the editor was given nothing to edit")
	}
	if got.Path == "" {
		t.Error("the editor cannot say where the script lives")
	}
	if got.Exists {
		t.Error("a site that never saved a script was told it had one")
	}
}

func TestHandleSiteDeployScript_SavesAndReadsBack(t *testing.T) {
	site := deployHome(t)

	body := strings.NewReader(`{"body":"#!/bin/sh\necho mine\n","backup":true}`)
	r := httptest.NewRequest(http.MethodPost, "/api/sites/shop.example/deploy-script", body)
	w := httptest.NewRecorder()
	handleSiteDeployScript(w, r, site)

	var saved SiteActionResponse
	if err := json.NewDecoder(w.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if !saved.OK {
		t.Fatalf("save failed: %+v", saved)
	}

	r = httptest.NewRequest(http.MethodGet, "/api/sites/shop.example/deploy-script", nil)
	w = httptest.NewRecorder()
	handleSiteDeployScript(w, r, site)

	var got SiteDeployScriptResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Exists || !strings.Contains(got.Body, "echo mine") {
		t.Errorf("the saved script did not come back: %+v", got)
	}
}
