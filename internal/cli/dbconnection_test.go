package cli

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/spf13/cobra"
)

func isolateInstall(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// The engine is chosen at install because it is the one database decision that
// is expensive to change later: moving a site between engines is a dump and a
// reload, not a setting.
func TestInstallDatabaseChoice_SetsTheDefault(t *testing.T) {
	isolateInstall(t)
	var installed string
	prev := ensureDatabaseService
	ensureDatabaseService = func(name string) error { installed = name; return nil }
	defer func() { ensureDatabaseService = prev }()

	cmd := NewInstallCmd()
	if err := cmd.Flags().Set("database", "postgres"); err != nil {
		t.Fatal(err)
	}
	if err := installDatabaseChoice(cmd); err != nil {
		t.Fatal(err)
	}

	if installed != "postgres" {
		t.Errorf("installed %q, want the engine that was chosen", installed)
	}
	c, err := dbconn.Default()
	if err != nil {
		t.Fatal(err)
	}
	if c.Service != "postgres" {
		t.Errorf("default connection = %+v, want the local postgres", c)
	}
}

// No choice is a choice: an install that says nothing keeps the behaviour it
// has always had, and does not start a database nobody asked for.
func TestInstallDatabaseChoice_SaysNothingWhenNothingIsAsked(t *testing.T) {
	isolateInstall(t)
	called := false
	prev := ensureDatabaseService
	ensureDatabaseService = func(string) error { called = true; return nil }
	defer func() { ensureDatabaseService = prev }()

	for _, choice := range []string{"", "none"} {
		cmd := NewInstallCmd()
		if err := cmd.Flags().Set("database", choice); err != nil {
			t.Fatal(err)
		}
		if err := installDatabaseChoice(cmd); err != nil {
			t.Fatalf("--database %q: %v", choice, err)
		}
	}
	if called {
		t.Error("a database was installed without being asked for")
	}
	reg, _ := dbconn.LoadRegistry()
	if reg.Default != "" {
		t.Errorf("default = %q, want none written down", reg.Default)
	}
}

// A typo installs nothing. Guessing which engine somebody meant is how a server
// ends up running two.
func TestInstallDatabaseChoice_RefusesAnEngineItDoesNotRun(t *testing.T) {
	isolateInstall(t)
	prev := ensureDatabaseService
	ensureDatabaseService = func(string) error {
		t.Error("an unknown engine reached the installer")
		return nil
	}
	defer func() { ensureDatabaseService = prev }()

	cmd := NewInstallCmd()
	_ = cmd.Flags().Set("database", "mongo")

	err := installDatabaseChoice(cmd)
	if err == nil {
		t.Fatal("an engine servlo does not run was accepted")
	}
	if !strings.Contains(err.Error(), "mysql") {
		t.Errorf("error = %q, does not say what the choices are", err)
	}
}

// MariaDB is a first-class choice rather than a MySQL-shaped afterthought.
func TestInstallDatabaseChoice_AcceptsMariaDB(t *testing.T) {
	isolateInstall(t)
	prev := ensureDatabaseService
	ensureDatabaseService = func(string) error { return nil }
	defer func() { ensureDatabaseService = prev }()

	cmd := NewInstallCmd()
	_ = cmd.Flags().Set("database", "mariadb")

	if err := installDatabaseChoice(cmd); err != nil {
		t.Fatal(err)
	}
	c, err := dbconn.Default()
	if err != nil {
		t.Fatal(err)
	}
	if c.Service != "mariadb" || c.Family != "mysql" {
		t.Errorf("connection = %+v, want the mariadb service speaking mysql", c)
	}
}

// A connection with sites on it is not removed out from under them: the sites
// name it, and the name would resolve to nothing.
func TestDbConnectionRemove_RefusesOneWithSitesOnIt(t *testing.T) {
	isolateInstall(t)

	reg := &dbconn.Registry{}
	if err := reg.Add(dbconn.External("managed", "mysql", "db.example.net", 3306, "admin", "pw")); err != nil {
		t.Fatal(err)
	}
	if err := dbconn.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}
	sites := &config.SiteRegistry{Sites: []config.Site{
		{Name: "shop", Domains: []string{"shop.example"}, Path: t.TempDir(), Database: "managed"},
	}}
	if err := config.SaveSites(sites); err != nil {
		t.Fatal(err)
	}

	err := runDbConnectionRemove("managed")

	if err == nil {
		t.Fatal("a connection with a site on it was removed")
	}
	if !strings.Contains(err.Error(), "shop.example") {
		t.Errorf("error = %q, does not name the site in the way", err)
	}
}

// Naming both a local service and a managed host is a request with two answers,
// and picking one of them silently is how a site ends up on the wrong database.
func TestDbConnectionAdd_RefusesBothKindsAtOnce(t *testing.T) {
	isolateInstall(t)

	err := runDbConnectionAdd("mix", "mysql", "postgres", "db.example.net", 5432, "admin", "", "")

	if err == nil {
		t.Fatal("a connection that was both local and managed was accepted")
	}
}

// Cobra wiring: the flag exists and is spelled the way the help says.
func TestInstallCmd_CarriesTheDatabaseFlag(t *testing.T) {
	cmd := NewInstallCmd()
	flag := cmd.Flags().Lookup("database")
	if flag == nil {
		t.Fatal("servlo install has no --database flag")
	}
	for _, engine := range []string{"mysql", "mariadb", "postgres", "none"} {
		if !strings.Contains(flag.Usage, engine) {
			t.Errorf("the help does not mention %q: %s", engine, flag.Usage)
		}
	}
}

var _ = cobra.Command{}
