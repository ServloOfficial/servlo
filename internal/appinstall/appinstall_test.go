package appinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/appstore"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
)

// sandbox isolates the registry so a test never reads or writes the machine's
// real sites.
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	return dir
}

// stub replaces every seam with something that succeeds and records, so each
// test can put one failure back and watch only that.
func stub(t *testing.T, app appstore.App) *calls {
	t.Helper()
	c := &calls{}

	restore := []func(){
		set(&loadApp, func(string) (appstore.App, error) { return app, nil }),
		set(&defaultConn, func() (dbconn.Connection, error) {
			return dbconn.LocalConnection("mysql", "mysql"), nil
		}),
		set(&createDatabase, func(svc, name string) (bool, error) {
			c.databases = append(c.databases, svc+"/"+name)
			return true, nil
		}),
		set(&ensureDBUser, func(conn dbconn.Connection, database string, grants []string) (dbconn.SiteUser, error) {
			return dbconn.SiteUser{User: database + "_u", Password: "generated-password"}, nil
		}),
		set(&fetchAndWrite, func(ctx context.Context, a appstore.App, req appstore.Request, deps appstore.Deps) (appstore.Result, error) {
			c.dir = req.Dir
			c.siteURL = req.SiteURL
			var res appstore.Result
			if a.Database.Required {
				conn, err := deps.CreateDatabase(req.DatabaseName)
				if err != nil {
					return res, err
				}
				res.Connection = conn
			}
			return res, nil
		}),
		set(&registerSite, func(site config.Site, php string) error {
			c.registered = append(c.registered, site.Name)
			return nil
		}),
		set(&runSetup, func(ctx context.Context, a appstore.App, url string, values map[string]string) error {
			c.setupValues = values
			return nil
		}),
		set(&generatePassword, func() (string, error) { return "admin-password", nil }),
	}
	t.Cleanup(func() {
		for _, undo := range restore {
			undo()
		}
	})
	return c
}

type calls struct {
	databases   []string
	registered  []string
	dir         string
	siteURL     string
	setupValues map[string]string
}

func set[T any](target *T, value T) func() {
	old := *target
	*target = value
	return func() { *target = old }
}

// withSetup is an app that ends with an account, the shape WordPress has.
func withSetup() appstore.App {
	return appstore.App{
		Name: "example", Label: "Example", Framework: "laravel",
		Database: appstore.Database{Required: true},
		Setup: appstore.Setup{
			Path: "/install.php", SuccessContains: "Success",
			Fields: map[string]string{"user": "{{admin_user}}"},
		},
	}
}

// withoutSetup is an app whose own installer servlo cannot drive.
func withoutSetup() appstore.App {
	return appstore.App{Name: "example", Label: "Example", Framework: "laravel"}
}

func TestInstall_EndsWithAnAdminAccountWhenTheAppHasASetupForm(t *testing.T) {
	sandbox(t)
	c := stub(t, withSetup())

	got, err := Install(t.Context(), Options{App: "example", Domain: "acme.test.example", Path: filepath.Join(t.TempDir(), "site")})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got.AdminPassword != "admin-password" {
		t.Errorf("the admin password is %q, so nothing can be shown to the operator", got.AdminPassword)
	}
	if got.AdminUser != "admin" {
		t.Errorf("admin user = %q, want the default", got.AdminUser)
	}
	if got.Note != "" {
		t.Errorf("the install finished the job and still reported something left to do: %s", got.Note)
	}
	if len(c.registered) != 1 {
		t.Errorf("the site was registered %d times", len(c.registered))
	}
	if c.setupValues["admin_password"] != "admin-password" {
		t.Errorf("the setup form was not given the generated password: %v", c.setupValues)
	}
}

// An app with no setup step is a legitimate app, and the one thing it must not
// do is finish quietly: an installer nobody has completed is an installer
// anybody who reaches it can complete.
func TestInstall_SaysWhatIsLeftWhenTheAppFinishesItself(t *testing.T) {
	sandbox(t)
	stub(t, withoutSetup())

	got, err := Install(t.Context(), Options{App: "example", Domain: "acme.test.example", Path: filepath.Join(t.TempDir(), "site")})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got.AdminPassword != "" {
		t.Error("an app with no setup form reported an admin password it never created")
	}
	if !strings.Contains(got.Note, "before pointing DNS") {
		t.Errorf("the note does not say to finish it before the domain is live: %q", got.Note)
	}
}

func TestInstall_RefusesADomainAlreadyServed(t *testing.T) {
	sandbox(t)
	stub(t, withSetup())
	if err := config.AddSite(config.Site{Name: "taken", Domains: []string{"acme.test.example"}, Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}

	_, err := Install(t.Context(), Options{App: "example", Domain: "acme.test.example", Path: filepath.Join(t.TempDir(), "site")})
	if err == nil || !strings.Contains(err.Error(), "already served by") {
		t.Fatalf("a second site on a live domain was accepted: %v", err)
	}
}

// Refused before the download, so a mistake costs a message rather than sixty
// megabytes unpacked over somebody's files.
func TestInstall_RefusesADirectoryWithAnythingInIt(t *testing.T) {
	sandbox(t)
	c := stub(t, withSetup())

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Install(t.Context(), Options{App: "example", Domain: "acme.test.example", Path: dir})
	if err == nil || !strings.Contains(err.Error(), "already has something in it") {
		t.Fatalf("an occupied directory was accepted: %v", err)
	}
	if c.dir != "" {
		t.Error("the release was fetched into a directory that was refused")
	}
}

// The site is serving by the time the form is posted, so a form that fails is
// reported rather than rolled back: taking the site away would lose the release
// and the database with it.
func TestInstall_KeepsTheSiteWhenTheSetupFormFails(t *testing.T) {
	sandbox(t)
	c := stub(t, withSetup())
	defer set(&runSetup, func(context.Context, appstore.App, string, map[string]string) error {
		return errors.New("the application said no")
	})()

	_, err := Install(t.Context(), Options{App: "example", Domain: "acme.test.example", Path: filepath.Join(t.TempDir(), "site")})
	if err == nil || !strings.Contains(err.Error(), "installed and serving") {
		t.Fatalf("a failed setup did not say the site is up: %v", err)
	}
	if len(c.registered) != 1 {
		t.Error("the site was not left registered after a failed setup")
	}
}

// A managed server holding an account servlo did not create is the one case it
// cannot answer for. Refusing beats writing a blank password into a config
// file, which either cannot connect or connects as whoever needs no password.
func TestProvision_RefusesAManagedAccountItDoesNotHoldThePasswordFor(t *testing.T) {
	sandbox(t)
	defer set(&provisionRemat, func(dbconn.Connection, string, string) (string, error) {
		return "", nil
	})()

	conn := dbconn.External("managed", "mysql", "db.example.com", 3306, "doadmin", "secret")
	_, err := provision(conn, "acme")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a blank password was accepted: %v", err)
	}
}

func TestProvision_UsesTheLocalPathForAConnectionServloHosts(t *testing.T) {
	sandbox(t)
	c := stub(t, withSetup())

	got, err := provision(dbconn.LocalConnection("mysql", "mysql"), "acme")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if len(c.databases) != 1 || c.databases[0] != "mysql/acme" {
		t.Errorf("the database was not created in the engine's own container: %v", c.databases)
	}
	if got.User == "" || got.Password == "" {
		t.Errorf("the application was handed credentials it cannot connect with: %+v", got)
	}
	if got.Host == "" {
		t.Error("the application was given no host to connect to")
	}
}
