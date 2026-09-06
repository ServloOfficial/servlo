package sitetpl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
)

// registerSite writes a sites.yaml holding one site, so ForSite resolves the
// same way it does at runtime.
func registerSite(t *testing.T, site config.Site) *config.Site {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)

	if site.Path == "" {
		site.Path = filepath.Join(t.TempDir(), site.Name)
	}
	if err := os.MkdirAll(site.Path, 0755); err != nil {
		t.Fatal(err)
	}
	reg := &config.SiteRegistry{Sites: []config.Site{site}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	return &loaded.Sites[0]
}

// The whole point of the placeholders: the same framework definition, written
// once, wires a site to whichever database that site is on.
func TestForSite_ResolvesTheSitesOwnConnection(t *testing.T) {
	site := registerSite(t, config.Site{Name: "shop", Domains: []string{"shop.example"}, Database: "managed"})

	// Two connections, and the site is on the one that is not the default, or
	// this passes just as well when the site's own choice is ignored.
	reg := &dbconn.Registry{}
	if err := reg.Add(dbconn.LocalConnection("local", "mysql")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(dbconn.External("managed", "postgres", "db.example.net", 25060, "doadmin", "s3cret")); err != nil {
		t.Fatal(err)
	}
	if reg.Default == site.Database {
		t.Fatalf("the site's connection is also the default, so this proves nothing")
	}
	if err := dbconn.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	got := Apply("DB_HOST={{db_host}} DB_PORT={{db_port}} DB_USERNAME={{db_user}} DB_PASSWORD={{db_password}}", ForSite(site))

	want := "DB_HOST=db.example.net DB_PORT=25060 DB_USERNAME=doadmin DB_PASSWORD=s3cret"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// A site that never chose one lands on the install's default, which on an
// install that has configured nothing is the local MySQL. That is where every
// site went before any of this existed, so nothing moves underneath anybody.
func TestForSite_NoChoiceMeansTheDefaultConnection(t *testing.T) {
	site := registerSite(t, config.Site{Name: "blog", Domains: []string{"blog.example"}})

	got := Apply("{{db_host}}:{{db_port}} as {{db_user}}", ForSite(site))

	if got != "servlo-mysql:3306 as root" {
		t.Errorf("got %q, want the local mysql service", got)
	}
}

// A connection string is the other shape a framework wires, and it has to come
// out as one thing rather than a host in one place and a port somewhere else.
func TestForSite_FillsAConnectionString(t *testing.T) {
	site := registerSite(t, config.Site{Name: "shop", Domains: []string{"shop.example"}, Database: "managed"})

	reg := &dbconn.Registry{}
	_ = reg.Add(dbconn.External("managed", "mysql", "db.example.net", 25060, "doadmin", "s3cret"))
	if err := dbconn.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	got := Apply("DATABASE_URL=mysql://{{db_user}}:{{db_password}}@{{db_host}}:{{db_port}}/{{site}}", ForSite(site))

	if got != "DATABASE_URL=mysql://doadmin:s3cret@db.example.net:25060/shop" {
		t.Errorf("got %q", got)
	}
}

// Once a site has its own database account, that is what its env file carries.
// The administrator's credentials in a site's .env are what let one site read
// every other site's data, which is the whole reason per-site accounts exist.
func TestForSite_PrefersTheSitesOwnDatabaseAccount(t *testing.T) {
	site := registerSite(t, config.Site{Name: "shop", Domains: []string{"shop.example"}, Database: "managed"})

	reg := &dbconn.Registry{}
	_ = reg.Add(dbconn.External("managed", "mysql", "db.example.net", 25060, "doadmin", "s3cret"))
	if err := dbconn.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}
	if err := dbconn.RecordSiteUser("managed", "shop", "shop", "sitepasswordsitepasswordsite"); err != nil {
		t.Fatal(err)
	}

	got := Apply("{{db_user}}:{{db_password}}", ForSite(site))

	if got != "shop:sitepasswordsitepasswordsite" {
		t.Errorf("got %q, want the site's own account", got)
	}
	if strings.Contains(got, "doadmin") || strings.Contains(got, "s3cret") {
		t.Errorf("got %q, which hands the site the administrator", got)
	}
}

// A site created before per-site accounts existed has none, and it must keep
// working exactly as it does today rather than losing its credentials.
func TestForSite_FallsBackToTheAdministratorWithoutAnAccount(t *testing.T) {
	site := registerSite(t, config.Site{Name: "legacy", Domains: []string{"legacy.example"}})

	got := Apply("{{db_user}}", ForSite(site))

	if got != "root" {
		t.Errorf("got %q, want the connection's administrator", got)
	}
}

// A connection servlo cannot resolve leaves the placeholder visible rather than
// writing a host it made up. A site pointed at a database that is not there
// should say so in its own env file, not connect to a different one.
func TestForSite_LeavesThePlaceholderWhenTheConnectionIsBroken(t *testing.T) {
	site := registerSite(t, config.Site{Name: "shop", Domains: []string{"shop.example"}})

	// A default naming a connection nobody configured.
	if err := dbconn.SaveRegistry(&dbconn.Registry{Default: "gone"}); err != nil {
		t.Fatal(err)
	}

	got := Apply("DB_HOST={{db_host}}", ForSite(site))

	if !strings.Contains(got, "{{db_host}}") {
		t.Errorf("got %q, want the placeholder left alone", got)
	}
	if strings.Contains(got, "servlo-mysql") {
		t.Errorf("got %q, which is a database the site was never pointed at", got)
	}
}
