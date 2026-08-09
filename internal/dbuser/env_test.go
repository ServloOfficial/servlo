package dbuser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
)

// registerSite writes a site and its env file the way a real install has them,
// so the rewrite resolves through the same framework definition `servlo env`
// used to write the credential in the first place.
func registerSite(t *testing.T, framework, env string) *config.Site {
	t.Helper()
	path := filepath.Join(t.TempDir(), "acme")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, ".env"), []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	site := config.Site{Name: "acme", Domains: []string{"acme.example"}, Path: path, Framework: framework}
	if err := config.SaveSites(&config.SiteRegistry{Sites: []config.Site{site}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	return &loaded.Sites[0]
}

const laravelEnv = `APP_NAME=Acme
DB_CONNECTION=mysql
DB_HOST=servlo-mysql
DB_PORT=3306
DB_DATABASE=acme
DB_USERNAME=root
DB_PASSWORD=oldadminpassword
`

// The keys a rotation rewrites are the ones the framework declares, so the
// panel does not have to know that Laravel calls it DB_USERNAME.
func TestWriteEnv_RewritesTheDeclaredCredentialKeys(t *testing.T) {
	isolate(t)
	site := registerSite(t, "laravel", laravelEnv)
	if err := dbconn.RecordSiteUser("mysql", "acme", "acme", "rotatedpasswordrotatedpassw"); err != nil {
		t.Fatal(err)
	}

	keys, err := WriteEnv(site)
	if err != nil {
		t.Fatal(err)
	}
	body := readEnv(t, site)
	if !strings.Contains(body, "DB_USERNAME=acme") {
		t.Errorf("the site's account did not reach the env file:\n%s", body)
	}
	if !strings.Contains(body, "DB_PASSWORD=rotatedpasswordrotatedpassw") {
		t.Errorf("the rotated password did not reach the env file:\n%s", body)
	}
	if strings.Contains(body, "oldadminpassword") {
		t.Errorf("the old credential is still in the file:\n%s", body)
	}
	if len(keys) != 2 {
		t.Errorf("keys = %v, want the two the framework declares", keys)
	}
}

// Everything else in the file is somebody's configuration, and a rotation has
// no business touching it.
func TestWriteEnv_LeavesEverythingElseAlone(t *testing.T) {
	isolate(t)
	site := registerSite(t, "laravel", laravelEnv)
	if err := dbconn.RecordSiteUser("mysql", "acme", "acme", "rotatedpasswordrotatedpassw"); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteEnv(site); err != nil {
		t.Fatal(err)
	}
	body := readEnv(t, site)
	for _, keep := range []string{"APP_NAME=Acme", "DB_HOST=servlo-mysql", "DB_DATABASE=acme"} {
		if !strings.Contains(body, keep) {
			t.Errorf("the rewrite dropped %q:\n%s", keep, body)
		}
	}
}

// A framework that carries the credential inside a connection string has one
// key rewritten whole, and it has to be the one for the dialect the site is
// actually on.
func TestEnvKeys_PicksTheDialectTheSiteIsOn(t *testing.T) {
	isolate(t)
	site := registerSite(t, "symfony", "DATABASE_URL=mysql://root:old@servlo-mysql:3306/acme\n")
	if err := dbconn.RecordSiteUser("mysql", "acme", "acme", "rotatedpasswordrotatedpassw"); err != nil {
		t.Fatal(err)
	}

	updates, err := EnvKeys(site)
	if err != nil {
		t.Fatal(err)
	}
	url, ok := updates["DATABASE_URL"]
	if !ok {
		t.Fatalf("no DATABASE_URL in %v", updates)
	}
	if !strings.HasPrefix(url, "mysql://acme:rotatedpasswordrotatedpassw@") {
		t.Errorf("DATABASE_URL = %q, want the site's own account on the dialect it is on", url)
	}
	if strings.Contains(url, "postgresql://") {
		t.Errorf("DATABASE_URL = %q, which is the wrong engine entirely", url)
	}
}

// A framework servlo manages no env file for is told plainly rather than
// silently reporting a rotation that landed nowhere.
func TestWriteEnv_SaysSoWhenThereIsNoEnvItManages(t *testing.T) {
	isolate(t)
	site := registerSite(t, "static", "")

	if _, err := WriteEnv(site); err == nil {
		t.Fatal("a site with no managed env file must report that its credentials need updating by hand")
	}
}

func readEnv(t *testing.T, site *config.Site) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(site.Path, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
