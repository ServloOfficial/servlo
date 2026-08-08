package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/authz"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/deploy"
	"gopkg.in/yaml.v3"
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

// deployWordPress is a WordPress site whose definition is in the local store,
// so its exclude list arrives from the store the way a real one does.
func deployWordPress(t *testing.T) *config.Site {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	body, err := os.ReadFile(filepath.Join("..", "..", "stores", "frameworks", "wordpress", "6.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var fw config.Framework
	if err := yaml.Unmarshal(body, &fw); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveStoreFramework(&fw); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"wp-login.php", "wp-config.php"} {
		if err := os.WriteFile(filepath.Join(path, f), []byte("<?php\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	site := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: path, PHPVersion: "8.3", Framework: "wordpress",
	}
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}
	return &site
}

func getExclude(t *testing.T, site *config.Site) SiteDeployExcludeResponse {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/sites/shop.example/deploy-exclude", nil)
	w := httptest.NewRecorder()
	handleSiteDeployExclude(w, r, site)
	var got SiteDeployExcludeResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("%v\n%s", err, w.Body.String())
	}
	return got
}

func postExclude(t *testing.T, site *config.Site, body string) SiteActionResponse {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/sites/shop.example/deploy-exclude", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleSiteDeployExclude(w, r, site)
	var got SiteActionResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("%v\n%s", err, w.Body.String())
	}
	return got
}

// A WordPress site protects uploads and plugins without anybody configuring it,
// and the form is told the list came from the framework rather than from here.
func TestHandleSiteDeployExclude_StartsFromTheFramework(t *testing.T) {
	site := deployWordPress(t)

	got := getExclude(t, site)

	if got.Custom {
		t.Error("a site that never set a list was reported as having its own")
	}
	for _, want := range []string{"wp-content/uploads", "wp-content/plugins"} {
		if !slices.Contains(got.Paths, want) {
			t.Errorf("Paths = %v, does not protect %q", got.Paths, want)
		}
		if !slices.Contains(got.Default, want) {
			t.Errorf("Default = %v, does not offer %q", got.Default, want)
		}
	}
}

func TestHandleSiteDeployExclude_SavesAndReadsBack(t *testing.T) {
	site := deployWordPress(t)

	if res := postExclude(t, site, `{"paths":["wp-content/uploads","wp-content/languages"]}`); !res.OK {
		t.Fatalf("save failed: %+v", res)
	}

	got := getExclude(t, site)
	if !got.Custom {
		t.Error("a site that saved a list was reported as still following its framework")
	}
	if !slices.Equal(got.Paths, []string{"wp-content/uploads", "wp-content/languages"}) {
		t.Errorf("Paths = %v", got.Paths)
	}
	// And the framework's list is still offered, because reset restores it.
	if !slices.Contains(got.Default, "wp-content/plugins") {
		t.Errorf("Default = %v, no longer offers the framework's list", got.Default)
	}
}

// Saving an empty list and resetting are different actions, and the difference
// survives: one protects nothing, the other goes back to the framework's list.
func TestHandleSiteDeployExclude_EmptyIsNotTheSameAsReset(t *testing.T) {
	site := deployWordPress(t)

	if res := postExclude(t, site, `{"paths":[]}`); !res.OK {
		t.Fatalf("save failed: %+v", res)
	}
	got := getExclude(t, site)
	if !got.Custom || len(got.Paths) != 0 {
		t.Errorf("an emptied list came back as %+v, want the site protecting nothing", got)
	}

	if res := postExclude(t, site, `{"reset":true}`); !res.OK {
		t.Fatalf("reset failed: %+v", res)
	}
	got = getExclude(t, site)
	if got.Custom {
		t.Error("a reset site is still reported as having its own list")
	}
	if !slices.Contains(got.Paths, "wp-content/plugins") {
		t.Errorf("Paths = %v, the framework's list did not come back", got.Paths)
	}
}

// A path that climbs out of the site decides where a deploy writes files, so it
// is refused at the door rather than stored and acted on later.
func TestHandleSiteDeployExclude_RefusesAPathOutsideTheSite(t *testing.T) {
	site := deployWordPress(t)

	res := postExclude(t, site, `{"paths":["../../etc"]}`)

	if res.OK {
		t.Fatal("a path outside the site was accepted")
	}
	if getExclude(t, site).Custom {
		t.Error("the refused list was stored anyway")
	}
}

func TestHandleSiteDeployExclude_RefusesOtherMethods(t *testing.T) {
	site := deployWordPress(t)

	r := httptest.NewRequest(http.MethodDelete, "/api/sites/shop.example/deploy-exclude", nil)
	w := httptest.NewRecorder()
	handleSiteDeployExclude(w, r, site)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func stubRedeploy(t *testing.T, res deploy.Result, err error) *[]string {
	t.Helper()
	var gotCommit []string
	prev := redeployFn
	redeployFn = func(o deploy.Options, commit string) (deploy.Result, error) {
		gotCommit = append(gotCommit, commit)
		if o.Out != nil {
			_, _ = o.Out.Write([]byte("going back\n"))
		}
		return res, err
	}
	t.Cleanup(func() { redeployFn = prev })
	return &gotCommit
}

// recorded captures what the handler writes to the history without needing one.
func recorded(t *testing.T) *[]deploy.Entry {
	t.Helper()
	var got []deploy.Entry
	prev := recordDeployFn
	recordDeployFn = func(_ *config.Site, e deploy.Entry) error {
		got = append(got, e)
		return nil
	}
	t.Cleanup(func() { recordDeployFn = prev })
	return &got
}

// Every deploy is recorded, because the redeploy that undoes it needs to know
// which commit the site was standing on.
func TestHandleSiteDeploy_RecordsWhatItDid(t *testing.T) {
	site := deployHome(t)
	entries := recorded(t)
	stubDeploy(t, deploy.Result{FromCommit: "aaaa1111", ToCommit: "bbbb2222", Snapshot: "snap-1"}, nil)

	postDeploy(t, site)

	if len(*entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(*entries))
	}
	e := (*entries)[0]
	if e.From != "aaaa1111" || e.To != "bbbb2222" || !e.OK {
		t.Errorf("entry = %+v", e)
	}
	if e.Redeploy {
		t.Error("a forward deploy was recorded as a redeploy")
	}
	if e.At.IsZero() {
		t.Error("the entry has no time, so a history cannot be ordered by it")
	}
}

// A failed deploy is recorded too. A history that only remembers the ones that
// worked cannot answer the question it actually gets asked.
func TestHandleSiteDeploy_RecordsAFailure(t *testing.T) {
	site := deployHome(t)
	entries := recorded(t)
	stubDeploy(t, deploy.Result{FromCommit: "aaaa1111", ToCommit: "bbbb2222"},
		errors.New("the deploy script failed"))

	postDeploy(t, site)

	if len(*entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(*entries))
	}
	if e := (*entries)[0]; e.OK || !strings.Contains(e.Error, "script failed") {
		t.Errorf("entry = %+v, want the failure and its reason", e)
	}
}

func getRedeploy(t *testing.T, site *config.Site) SiteRedeployResponse {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/sites/shop.example/redeploy", nil)
	w := httptest.NewRecorder()
	handleSiteRedeploy(w, r, site)
	var got SiteRedeployResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("%v\n%s", err, w.Body.String())
	}
	return got
}

// The panel is told the target commit before the operator presses anything, so
// the button is not a surprise.
func TestHandleSiteRedeploy_SaysWhereItWouldGo(t *testing.T) {
	site := deployHome(t)
	if err := deploy.Record(site, deploy.Entry{From: "aaaa1111", To: "bbbb2222", OK: true}); err != nil {
		t.Fatal(err)
	}

	got := getRedeploy(t, site)

	if !got.Available || got.Commit != "aaaa1111" {
		t.Errorf("GET = %+v, want the commit the site was on", got)
	}
}

// A site that never deployed has nowhere to go back to, and says so rather than
// offering a button that would fail.
func TestHandleSiteRedeploy_UnavailableBeforeAnyDeploy(t *testing.T) {
	site := deployHome(t)

	if got := getRedeploy(t, site); got.Available {
		t.Errorf("GET = %+v, want unavailable", got)
	}

	calls := stubRedeploy(t, deploy.Result{}, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/sites/shop.example/redeploy", nil)
	w := httptest.NewRecorder()
	handleSiteRedeploy(w, r, site)

	if len(*calls) != 0 {
		t.Error("a redeploy ran with no commit to go back to")
	}
	if !strings.Contains(w.Body.String(), "no recorded deploy") {
		t.Errorf("the refusal does not say why:\n%s", w.Body.String())
	}
}

// It goes back to the recorded commit, not to whatever the caller asked for:
// the target comes from the history, so a request cannot name an arbitrary one.
func TestHandleSiteRedeploy_GoesToTheRecordedCommit(t *testing.T) {
	site := deployHome(t)
	for _, e := range []deploy.Entry{
		{From: "aaaa1111", To: "bbbb2222", OK: true},
		{From: "bbbb2222", To: "cccc3333", OK: true},
	} {
		if err := deploy.Record(site, e); err != nil {
			t.Fatal(err)
		}
	}
	calls := stubRedeploy(t, deploy.Result{FromCommit: "cccc3333", ToCommit: "bbbb2222"}, nil)
	entries := recorded(t)

	r := httptest.NewRequest(http.MethodPost, "/api/sites/shop.example/redeploy?commit=deadbeef", nil)
	w := httptest.NewRecorder()
	handleSiteRedeploy(w, r, site)

	if len(*calls) != 1 || (*calls)[0] != "bbbb2222" {
		t.Errorf("redeploy targets = %v, want the last recorded From", *calls)
	}
	got := doneFrame(t, w.Body.String())
	if got["ok"] != true || got["to"] != "bbbb2222" {
		t.Errorf("done = %v", got)
	}
	// And the redeploy is itself recorded, marked as one.
	if len(*entries) != 1 || !(*entries)[0].Redeploy {
		t.Errorf("entries = %+v, want one marked as a redeploy", *entries)
	}
}

func TestHandleSiteRedeploy_RefusesWhileTheSiteIsBusy(t *testing.T) {
	site := deployHome(t)
	if err := deploy.Record(site, deploy.Entry{From: "aaaa1111", To: "bbbb2222", OK: true}); err != nil {
		t.Fatal(err)
	}
	calls := stubRedeploy(t, deploy.Result{}, nil)

	release, _, ok := tryAcquireRun(siteRunLockKey(site), "deploy")
	if !ok {
		t.Fatal("could not take the lock")
	}
	defer release()

	r := httptest.NewRequest(http.MethodPost, "/api/sites/shop.example/redeploy", nil)
	w := httptest.NewRecorder()
	handleSiteRedeploy(w, r, site)

	if len(*calls) != 0 {
		t.Error("a redeploy started while the site was busy")
	}
	if !strings.Contains(w.Body.String(), "deploy") {
		t.Errorf("the refusal does not say what it is busy with:\n%s", w.Body.String())
	}
}

func TestHandleSiteRedeploy_RefusesOtherMethods(t *testing.T) {
	site := deployHome(t)

	r := httptest.NewRequest(http.MethodDelete, "/api/sites/shop.example/redeploy", nil)
	w := httptest.NewRecorder()
	handleSiteRedeploy(w, r, site)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// The history has to say which change went out and who sent it, not just when.
func TestHandleSiteDeploy_RecordsTheAuthorAndTheActor(t *testing.T) {
	site := deployHome(t)
	entries := recorded(t)
	stubDeploy(t, deploy.Result{FromCommit: "aaaa1111", ToCommit: "bbbb2222"}, nil)

	r := httptest.NewRequest(http.MethodPost, "/api/sites/shop.example/deploy", nil)
	r = r.WithContext(authz.WithSession(r.Context(), authz.Session{User: "alice"}))
	handleSiteDeploy(httptest.NewRecorder(), r, site)

	if len(*entries) != 1 {
		t.Fatalf("recorded %d entries", len(*entries))
	}
	if got := (*entries)[0].Actor; got != "alice" {
		t.Errorf("actor = %q, want the signed-in user", got)
	}
}

// A deploy with no session has no actor and says so, rather than borrowing a
// name. That is the shape a webhook deploy will arrive in.
func TestHandleSiteDeploy_NoSessionMeansNoActor(t *testing.T) {
	site := deployHome(t)
	entries := recorded(t)
	stubDeploy(t, deploy.Result{FromCommit: "aaaa1111", ToCommit: "bbbb2222"}, nil)

	postDeploy(t, site)

	if len(*entries) != 1 {
		t.Fatalf("recorded %d entries", len(*entries))
	}
	if got := (*entries)[0].Actor; got != "" {
		t.Errorf("actor = %q, want empty", got)
	}
}

func TestHandleSiteDeployHistory_ReturnsTheDeploysNewestFirst(t *testing.T) {
	site := deployHome(t)
	for _, e := range []deploy.Entry{
		{From: "aaaa1111", To: "bbbb2222", OK: true, Subject: "first"},
		{From: "bbbb2222", To: "cccc3333", OK: false, Error: "boom", Subject: "second"},
	} {
		if err := deploy.Record(site, e); err != nil {
			t.Fatal(err)
		}
	}

	r := httptest.NewRequest(http.MethodGet, "/api/sites/shop.example/deploy-history", nil)
	w := httptest.NewRecorder()
	handleSiteDeployHistory(w, r, site)

	var got SiteDeployHistoryResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("%v\n%s", err, w.Body.String())
	}
	if len(got.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(got.Entries))
	}
	if got.Entries[0].Subject != "second" {
		t.Errorf("entries are not newest first: %+v", got.Entries)
	}
	// The failure is in the list, with its reason. A history that hid failures
	// would be missing the entries anyone actually goes looking for.
	if got.Entries[0].OK || got.Entries[0].Error != "boom" {
		t.Errorf("the failed deploy lost its outcome: %+v", got.Entries[0])
	}
}

// An empty history is a list, not null, so the panel renders it without
// knowing that JSON has two ways to say nothing.
func TestHandleSiteDeployHistory_EmptyIsAList(t *testing.T) {
	site := deployHome(t)

	r := httptest.NewRequest(http.MethodGet, "/api/sites/shop.example/deploy-history", nil)
	w := httptest.NewRecorder()
	handleSiteDeployHistory(w, r, site)

	if !strings.Contains(w.Body.String(), `"entries":[]`) {
		t.Errorf("body = %s, want an empty list", w.Body.String())
	}
}

func TestHandleSiteDeployHistory_RefusesOtherMethods(t *testing.T) {
	site := deployHome(t)

	r := httptest.NewRequest(http.MethodPost, "/api/sites/shop.example/deploy-history", nil)
	w := httptest.NewRecorder()
	handleSiteDeployHistory(w, r, site)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}
