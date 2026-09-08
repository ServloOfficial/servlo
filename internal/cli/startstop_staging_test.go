package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/nginx"
)

// A staging site is behind a password, and the file nginx checks that password
// against lives under the data directory, which the server-state archive does
// not carry. So a rebuild put the registry back with the hash still in it,
// regenerated a vhost naming an auth file, and never wrote the file. nginx
// answers 500 to every request against an auth_basic_user_file it cannot open,
// so every staging site on the rebuilt machine was broken rather than closed,
// and the hash that would have fixed it was sitting in sites.yaml being read by
// nothing.
func TestRestoreStagingCredentials_WritesTheFileNginxChecks(t *testing.T) {
	isolate(t)

	site := config.Site{
		Name: "acme-staging", Domains: []string{"staging.acme-supply.com"}, Path: t.TempDir(),
		Staging: &config.SiteStaging{
			Origin: "acme", User: "staging",
			Hash: "$2y$05$abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTU",
		},
	}

	restoreStagingCredentials(site)

	body, err := os.ReadFile(nginx.HtpasswdPath("staging.acme-supply.com"))
	if err != nil {
		t.Fatalf("nginx's auth file was not restored, so the site answers 500: %v", err)
	}
	want := "staging:" + site.Staging.Hash + "\n"
	if string(body) != want {
		t.Errorf("auth file = %q, want %q", body, want)
	}
}

// The file is authoritative once it exists. Rewriting it on every start would
// undo a password set between starts if the registry ever fell behind, and
// there is nothing to gain: the content is already what the hash says.
func TestRestoreStagingCredentials_LeavesAnExistingFileAlone(t *testing.T) {
	isolate(t)

	site := config.Site{
		Name: "acme-staging", Domains: []string{"staging.acme-supply.com"}, Path: t.TempDir(),
		Staging: &config.SiteStaging{Origin: "acme", User: "staging", Hash: "$2y$05$newhash"},
	}

	if err := os.MkdirAll(config.NginxHtpasswdDir(), 0755); err != nil {
		t.Fatal(err)
	}
	existing := "staging:$2y$05$whatevernginxisalreadychecking\n"
	if err := os.WriteFile(nginx.HtpasswdPath("staging.acme-supply.com"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	restoreStagingCredentials(site)

	body, err := os.ReadFile(nginx.HtpasswdPath("staging.acme-supply.com"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != existing {
		t.Errorf("an existing auth file was overwritten: got %q, want %q", body, existing)
	}
}

// A site that is not staging has no password and must not acquire a stray file
// under a name a later staging site could be given.
func TestRestoreStagingCredentials_IgnoresAnOrdinarySite(t *testing.T) {
	isolate(t)

	site := config.Site{Name: "acme", Domains: []string{"acme-supply.com"}, Path: t.TempDir()}

	restoreStagingCredentials(site)

	if _, err := os.Stat(nginx.HtpasswdPath("acme-supply.com")); err == nil {
		t.Error("an ordinary site was given an auth file")
	} else if !strings.Contains(err.Error(), "no such file") {
		t.Fatal(err)
	}
}

// A staging site created before the hash was kept in the registry has nothing
// to write. It stays not-indexed and open rather than 500, which is what the
// vhost already does for a staging block with no user, and it must not be
// turned into a broken site by a half-written auth file.
func TestRestoreStagingCredentials_SkipsASiteWithNoHash(t *testing.T) {
	isolate(t)

	site := config.Site{
		Name: "old-staging", Domains: []string{"staging.old.example"}, Path: t.TempDir(),
		Staging: &config.SiteStaging{Origin: "old", User: "staging"},
	}

	restoreStagingCredentials(site)

	if _, err := os.Stat(nginx.HtpasswdPath("staging.old.example")); err == nil {
		t.Error("an auth file was written with no hash to put in it")
	}
}
