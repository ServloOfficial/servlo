package ui

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/authz"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/deploy"
	"github.com/realrashid/servlo/internal/webhook"
)

const pushBody = `{"ref":"refs/heads/main","after":"bbbb2222","head_commit":{"id":"bbbb2222","message":"add the orders index"}}`

func hookHome(t *testing.T) (*config.Site, config.SiteWebhookConfig) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	path := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	site := config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: path, PHPVersion: "8.4"}
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}
	hook, err := config.EnableSiteWebhook(&site, "main")
	if err != nil {
		t.Fatal(err)
	}
	return &site, hook
}

func signed(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// post sends a push the way a git host would, with whatever overrides a test
// needs.
func post(t *testing.T, hook config.SiteWebhookConfig, body, sig, delivery string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/webhooks/deploy/"+hook.ID, strings.NewReader(body))
	if sig != "" {
		r.Header.Set(webhook.SignatureHeader, sig)
	}
	if delivery != "" {
		r.Header.Set(webhook.DeliveryHeader, delivery)
	}
	w := httptest.NewRecorder()
	handleWebhookDeploy(w, r)
	return w
}

func stubWebhookDeploy(t *testing.T) *int {
	t.Helper()
	runs := 0
	prev := runDeployFn
	runDeployFn = func(o deploy.Options) (deploy.Result, error) {
		runs++
		return deploy.Result{FromCommit: "aaaa1111", ToCommit: "bbbb2222"}, nil
	}
	t.Cleanup(func() { runDeployFn = prev })
	return &runs
}

func TestHandleWebhookDeploy_DeploysASignedPush(t *testing.T) {
	site, hook := hookHome(t)
	runs := stubWebhookDeploy(t)

	w := post(t, hook, pushBody, signed(hook.Secret, pushBody), "delivery-1")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d: %s", w.Code, w.Body.String())
	}
	if *runs != 1 {
		t.Errorf("the deploy ran %d times, want once", *runs)
	}
	// And it is recorded as a deploy nobody signed in triggered.
	entries, err := deploy.History(site)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("recorded %d entries", len(entries))
	}
	if entries[0].Actor != "" {
		t.Errorf("actor = %q, want none for a webhook deploy", entries[0].Actor)
	}
}

// The signature is the whole authentication, so every way of getting it wrong
// has to be a refusal that deploys nothing.
func TestHandleWebhookDeploy_RefusesAnythingUnsigned(t *testing.T) {
	_, hook := hookHome(t)
	runs := stubWebhookDeploy(t)

	cases := map[string]struct{ body, sig string }{
		"no signature":      {pushBody, ""},
		"the wrong secret":  {pushBody, signed("not-the-secret", pushBody)},
		"a modified branch": {strings.Replace(pushBody, "main", "evil", 1), signed(hook.Secret, pushBody)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := post(t, hook, c.body, c.sig, "delivery-"+name)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: %s", w.Code, w.Body.String())
			}
		})
	}
	if *runs != 0 {
		t.Errorf("an unsigned push deployed (%d times)", *runs)
	}
}

// An unknown endpoint is a 404 and nothing else. Saying "no such site" would
// turn the URL into an oracle for which sites exist.
func TestHandleWebhookDeploy_UnknownEndpointSaysNothing(t *testing.T) {
	hookHome(t)
	runs := stubWebhookDeploy(t)

	r := httptest.NewRequest(http.MethodPost, "/api/webhooks/deploy/not-a-real-id", strings.NewReader(pushBody))
	r.Header.Set(webhook.DeliveryHeader, "delivery-1")
	w := httptest.NewRecorder()
	handleWebhookDeploy(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "site") {
		t.Errorf("the refusal describes what was not found: %s", w.Body.String())
	}
	if *runs != 0 {
		t.Error("an unknown endpoint deployed something")
	}
}

// A push to another branch is not an error. Answering 4xx or 5xx makes the git
// host retry and eventually disable the hook, so an ignored push says so with a
// 200.
func TestHandleWebhookDeploy_IgnoresAnotherBranch(t *testing.T) {
	_, hook := hookHome(t)
	runs := stubWebhookDeploy(t)

	body := strings.Replace(pushBody, "refs/heads/main", "refs/heads/feature/checkout", 1)
	w := post(t, hook, body, signed(hook.Secret, body), "delivery-1")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 so the host does not retry", w.Code)
	}
	if !strings.Contains(w.Body.String(), "feature/checkout") {
		t.Errorf("the response does not say which branch was ignored: %s", w.Body.String())
	}
	if *runs != 0 {
		t.Error("a push to another branch deployed")
	}
}

// A tag push carries no branch and never deploys.
func TestHandleWebhookDeploy_IgnoresATagPush(t *testing.T) {
	_, hook := hookHome(t)
	runs := stubWebhookDeploy(t)

	body := strings.Replace(pushBody, "refs/heads/main", "refs/tags/v1.2.0", 1)
	w := post(t, hook, body, signed(hook.Secret, body), "delivery-1")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	if *runs != 0 {
		t.Error("a tag push deployed")
	}
}

// The same delivery twice is one deploy. A git host retries, and a captured
// body stays valid forever.
func TestHandleWebhookDeploy_RefusesAReplay(t *testing.T) {
	_, hook := hookHome(t)
	runs := stubWebhookDeploy(t)
	sig := signed(hook.Secret, pushBody)

	if w := post(t, hook, pushBody, sig, "delivery-1"); w.Code != http.StatusOK {
		t.Fatalf("the first delivery failed: %d %s", w.Code, w.Body.String())
	}
	w := post(t, hook, pushBody, sig, "delivery-1")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 so the host stops retrying", w.Code)
	}
	if *runs != 1 {
		t.Errorf("the deploy ran %d times, want once", *runs)
	}
}

// A disabled webhook does not answer, which is what "off by default" has to
// mean once the credentials have been forgotten.
func TestHandleWebhookDeploy_DisabledEndpointIsGone(t *testing.T) {
	site, hook := hookHome(t)
	runs := stubWebhookDeploy(t)
	if err := config.DisableSiteWebhook(site); err != nil {
		t.Fatal(err)
	}

	w := post(t, hook, pushBody, signed(hook.Secret, pushBody), "delivery-1")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if *runs != 0 {
		t.Error("a disabled webhook deployed")
	}
}

// A body big enough to exhaust memory is refused before it is read. This
// endpoint takes unauthenticated bytes from the internet, which nothing else
// in the panel does.
func TestHandleWebhookDeploy_RefusesAnOversizedBody(t *testing.T) {
	_, hook := hookHome(t)
	runs := stubWebhookDeploy(t)

	huge := `{"ref":"refs/heads/main","pad":"` + strings.Repeat("x", maxWebhookBody+1024) + `"}`
	w := post(t, hook, huge, signed(hook.Secret, huge), "delivery-1")

	if w.Code == http.StatusOK {
		t.Error("an oversized body was accepted")
	}
	if *runs != 0 {
		t.Error("an oversized body deployed")
	}
}

func TestHandleWebhookDeploy_RefusesOtherMethods(t *testing.T) {
	_, hook := hookHome(t)
	runs := stubWebhookDeploy(t)

	r := httptest.NewRequest(http.MethodGet, "/api/webhooks/deploy/"+hook.ID, nil)
	w := httptest.NewRecorder()
	handleWebhookDeploy(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if *runs != 0 {
		t.Error("a GET deployed")
	}
}

// The secret never leaves the server through the panel API either. An operator
// sets it up once and can regenerate it; reading it back is not a thing the
// panel needs to do.
func TestHandleSiteWebhook_NeverReturnsTheSecretTwice(t *testing.T) {
	site, _ := hookHome(t)

	r := httptest.NewRequest(http.MethodGet, "/api/sites/shop.example/webhook", nil)
	w := httptest.NewRecorder()
	handleSiteWebhook(w, r, site)

	var got SiteWebhookResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Secret != "" {
		t.Errorf("the GET handed back the secret: %q", got.Secret)
	}
	if !got.Enabled || got.URL == "" {
		t.Errorf("response = %+v, want an enabled webhook with its URL", got)
	}
}

// Enabling returns the secret exactly once, because that is the only moment it
// can be copied into the repository's settings.
func TestHandleSiteWebhook_ShowsTheSecretWhenItIsMinted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	path := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	site := &config.Site{Name: "shop", Domains: []string{"shop.example"}, Path: path}
	if err := config.AddSite(*site); err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(`{"enabled":true,"branch":"main"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/sites/shop.example/webhook", body)
	w := httptest.NewRecorder()
	handleSiteWebhook(w, r, site)

	var got SiteWebhookResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("%v\n%s", err, w.Body.String())
	}
	if got.Secret == "" {
		t.Error("enabling did not show the secret, so it can never be configured")
	}
	if got.Branch != "main" {
		t.Errorf("Branch = %q", got.Branch)
	}
}

// The path is written in three places: the mux registration, the permission
// registry, and the constant that builds the URL the panel shows. The scan
// checks the first two agree; this checks the third does, so the URL an
// operator pastes into GitHub is the one that is actually routed and declared.
func TestWebhookPathPrefix_MatchesItsPermissionDeclaration(t *testing.T) {
	permission, ok := authz.Permissions().For(webhookPathPrefix)
	if !ok {
		t.Fatalf("%q declares no permission", webhookPathPrefix)
	}
	if permission != authz.PermPublic {
		t.Errorf("permission = %q, want public: a git host carries no session", permission)
	}
}
