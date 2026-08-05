package dnsprovider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func credsEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	return tmp
}

// A DNS API token can create and delete any record in the zone, which for most
// operators means it can take the domain over completely. It is the most
// dangerous secret servlo stores, so its file is owner-only from the moment it
// exists rather than chmodded afterwards.
func TestSaveCredentials_FileIsOwnerOnly(t *testing.T) {
	credsEnv(t)

	if err := SaveCredentials(Credentials{Provider: Cloudflare, APIToken: "cf-secret"}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	info, err := os.Stat(credentialsPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials mode = %#o, want 0600", perm)
	}
	dir, err := os.Stat(filepath.Dir(credentialsPath()))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dir.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("credentials directory mode = %#o, want no group or other access", perm)
	}
}

// The acceptance criterion is explicit: never inside a site directory. A site
// directory is served by nginx, cloned from git, and often owned by an app that
// can read its own tree, so a token there is one misconfigured location block
// from being downloadable.
func TestCredentialsPath_IsNeverInsideASiteDirectory(t *testing.T) {
	cfgHome := credsEnv(t)

	path := credentialsPath()
	if !strings.HasPrefix(path, filepath.Join(cfgHome, "servlo")) {
		t.Errorf("credentials at %q, want them under the servlo config directory", path)
	}
	for _, siteish := range []string{"/srv/", "/var/www/", "/home/user/sites"} {
		if strings.Contains(path, siteish) {
			t.Errorf("credentials path %q looks like it is inside a site tree", path)
		}
	}
}

func TestCredentials_RoundTripPerProvider(t *testing.T) {
	credsEnv(t)

	all := []Credentials{
		{Provider: Cloudflare, APIToken: "cf-token"},
		{Provider: DigitalOcean, APIToken: "do-token"},
		{Provider: Route53, AccessKeyID: "AKIA", SecretAccessKey: "secret", Region: "us-east-1"},
	}
	for _, c := range all {
		if err := SaveCredentials(c); err != nil {
			t.Fatalf("SaveCredentials(%s): %v", c.Provider, err)
		}
	}
	for _, want := range all {
		got, err := LoadCredentials(want.Provider)
		if err != nil {
			t.Fatalf("LoadCredentials(%s): %v", want.Provider, err)
		}
		if got != want {
			t.Errorf("round trip for %s = %+v, want %+v", want.Provider, got, want)
		}
	}
}

// Saving one provider must not drop the others, since an operator may hold
// domains across two registrars.
func TestSaveCredentials_KeepsTheOtherProviders(t *testing.T) {
	credsEnv(t)

	if err := SaveCredentials(Credentials{Provider: Cloudflare, APIToken: "cf"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(Credentials{Provider: DigitalOcean, APIToken: "do"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCredentials(Cloudflare)
	if err != nil {
		t.Fatalf("the cloudflare credentials were lost: %v", err)
	}
	if got.APIToken != "cf" {
		t.Errorf("cloudflare token = %q, want it untouched", got.APIToken)
	}
}

// An unconfigured provider has to say so rather than returning a blank
// credential that fails later as an opaque 401 from the registrar.
func TestLoadCredentials_UnconfiguredSaysSo(t *testing.T) {
	credsEnv(t)

	_, err := LoadCredentials(Route53)
	if err == nil {
		t.Fatal("an unconfigured provider loaded without error")
	}
	if !strings.Contains(err.Error(), string(Route53)) {
		t.Errorf("error %q does not name the provider", err)
	}
}

func TestValidate_RejectsIncompleteCredentials(t *testing.T) {
	cases := []struct {
		name string
		in   Credentials
	}{
		{"cloudflare with no token", Credentials{Provider: Cloudflare}},
		{"digitalocean with no token", Credentials{Provider: DigitalOcean}},
		{"route53 with no secret", Credentials{Provider: Route53, AccessKeyID: "AKIA"}},
		{"route53 with no key id", Credentials{Provider: Route53, SecretAccessKey: "s"}},
		{"unknown provider", Credentials{Provider: "gandi", APIToken: "t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.in.Validate(); err == nil {
				t.Error("accepted incomplete credentials")
			}
		})
	}
}

// Secrets are redacted everywhere they might be printed. A token in a deploy
// log or an error message outlives the moment it was written.
func TestCredentials_RedactSecretsWhenPrinted(t *testing.T) {
	c := Credentials{Provider: Route53, AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "super-secret", Region: "eu-west-1"}
	rendered := c.String()
	for _, secret := range []string{"super-secret", "AKIAEXAMPLE"} {
		if strings.Contains(rendered, secret) {
			t.Errorf("String() leaked %q: %s", secret, rendered)
		}
	}
	if !strings.Contains(rendered, "route53") {
		t.Errorf("String() does not identify the provider: %s", rendered)
	}

	cf := Credentials{Provider: Cloudflare, APIToken: "cf-token-value"}
	if strings.Contains(cf.String(), "cf-token-value") {
		t.Errorf("String() leaked the API token: %s", cf.String())
	}
}

// Configured() is what the panel asks to decide whether to offer DNS-01, so it
// must not be the thing that hands a secret to a template.
func TestConfigured_ListsProvidersWithoutTheirSecrets(t *testing.T) {
	credsEnv(t)
	if err := SaveCredentials(Credentials{Provider: Cloudflare, APIToken: "cf"}); err != nil {
		t.Fatal(err)
	}

	got := Configured()
	if len(got) != 1 || got[0] != Cloudflare {
		t.Errorf("Configured() = %v, want just cloudflare", got)
	}
}
