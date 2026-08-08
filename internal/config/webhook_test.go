package config

import (
	"os"
	"path/filepath"
	"testing"
)

func webhookHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
}

// Off until somebody turns it on. A deploy endpoint that existed the moment a
// site did would be a deploy endpoint on every site whose operator never
// thought about it.
func TestSiteWebhook_OffByDefault(t *testing.T) {
	webhookHome(t)
	site := &Site{Name: "shop", Domains: []string{"shop.example"}}

	hook, err := SiteWebhook(site)
	if err != nil {
		t.Fatal(err)
	}
	if hook.Enabled {
		t.Error("a site that nobody configured has a live deploy webhook")
	}
	if hook.Secret != "" || hook.ID != "" {
		t.Errorf("a disabled webhook already has credentials: %+v", hook)
	}
}

// Enabling mints both halves: a public identifier for the URL and a secret for
// the signature. They are separate so the URL can be pasted into GitHub's
// settings, and read back, without handing over the thing that authenticates.
func TestEnableSiteWebhook_MintsAnIDAndASecret(t *testing.T) {
	webhookHome(t)
	site := &Site{Name: "shop", Domains: []string{"shop.example"}}

	hook, err := EnableSiteWebhook(site, "main")
	if err != nil {
		t.Fatal(err)
	}

	if !hook.Enabled {
		t.Error("enabling did not enable it")
	}
	if len(hook.ID) < 16 {
		t.Errorf("ID = %q, too short to be unguessable", hook.ID)
	}
	if len(hook.Secret) < 32 {
		t.Errorf("secret = %q, too short", hook.Secret)
	}
	if hook.ID == hook.Secret {
		t.Error("the public identifier and the secret are the same string")
	}
	if hook.Branch != "main" {
		t.Errorf("Branch = %q", hook.Branch)
	}

	back, err := SiteWebhook(site)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != hook.ID || back.Secret != hook.Secret {
		t.Errorf("read back %+v, want %+v", back, hook)
	}
}

// Two sites never share a secret. One site's repository learning the token that
// deploys another is the whole reason this is per site.
func TestEnableSiteWebhook_PerSiteSecrets(t *testing.T) {
	webhookHome(t)
	a := &Site{Name: "shop", Domains: []string{"shop.example"}}
	b := &Site{Name: "blog", Domains: []string{"blog.example"}}

	ha, err := EnableSiteWebhook(a, "main")
	if err != nil {
		t.Fatal(err)
	}
	hb, err := EnableSiteWebhook(b, "main")
	if err != nil {
		t.Fatal(err)
	}

	if ha.Secret == hb.Secret {
		t.Error("two sites were given the same webhook secret")
	}
	if ha.ID == hb.ID {
		t.Error("two sites were given the same webhook id")
	}
}

// The file holds tokens that deploy code to a server. Nobody else on the box
// needs to read it.
func TestWebhookStore_IsNotWorldReadable(t *testing.T) {
	webhookHome(t)
	site := &Site{Name: "shop", Domains: []string{"shop.example"}}
	if _, err := EnableSiteWebhook(site, "main"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(WebhookFile())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %04o, want 0600", perm)
	}
	// And it is not the site registry, which is world readable by design.
	if WebhookFile() == SitesFile() {
		t.Error("webhook secrets are stored in the world-readable site registry")
	}
	if dir := filepath.Dir(WebhookFile()); dir == "" {
		t.Error("no directory for the webhook store")
	}
}

// Regenerating replaces the secret and keeps the URL, which is the operation an
// operator wants after a secret leaks: the endpoint they already pasted into
// GitHub stays valid and every old signature stops working.
func TestRegenerateSiteWebhookSecret_KeepsTheURL(t *testing.T) {
	webhookHome(t)
	site := &Site{Name: "shop", Domains: []string{"shop.example"}}
	first, err := EnableSiteWebhook(site, "main")
	if err != nil {
		t.Fatal(err)
	}

	second, err := RegenerateSiteWebhookSecret(site)
	if err != nil {
		t.Fatal(err)
	}

	if second.ID != first.ID {
		t.Errorf("the endpoint changed: %q then %q", first.ID, second.ID)
	}
	if second.Secret == first.Secret {
		t.Error("regenerating produced the same secret")
	}
}

// Disabling stops it answering and forgets the credentials, so re-enabling
// later cannot resurrect a token that was disabled because it leaked.
func TestDisableSiteWebhook_ForgetsTheCredentials(t *testing.T) {
	webhookHome(t)
	site := &Site{Name: "shop", Domains: []string{"shop.example"}}
	before, err := EnableSiteWebhook(site, "main")
	if err != nil {
		t.Fatal(err)
	}

	if err := DisableSiteWebhook(site); err != nil {
		t.Fatal(err)
	}

	hook, err := SiteWebhook(site)
	if err != nil {
		t.Fatal(err)
	}
	if hook.Enabled || hook.Secret != "" || hook.ID != "" {
		t.Errorf("disabled webhook kept something: %+v", hook)
	}

	again, err := EnableSiteWebhook(site, "main")
	if err != nil {
		t.Fatal(err)
	}
	if again.Secret == before.Secret {
		t.Error("re-enabling brought back the old secret")
	}
}

// The lookup a request does: an ID off the wire has to find its site, and an
// unknown one has to find nothing.
func TestSiteByWebhookID(t *testing.T) {
	webhookHome(t)
	site := &Site{Name: "shop", Domains: []string{"shop.example"}}
	hook, err := EnableSiteWebhook(site, "main")
	if err != nil {
		t.Fatal(err)
	}

	name, found, err := SiteNameByWebhookID(hook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || name != "shop" {
		t.Errorf("SiteNameByWebhookID = %q, %v", name, found)
	}

	if _, found, err = SiteNameByWebhookID("nope"); err != nil || found {
		t.Error("an unknown id matched a site")
	}
	if _, found, err = SiteNameByWebhookID(""); err != nil || found {
		t.Error("an empty id matched a site")
	}
}
