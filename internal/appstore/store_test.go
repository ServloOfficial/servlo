package appstore

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/realrashid/servlo/stores"
)

// The guard that matters most in this package. Every definition servlo ships
// goes through the same validation an operator-supplied one would, so a typo in
// a checksum or a path that escapes the site fails the build rather than an
// install on somebody's server.
func TestList_EveryShippedDefinitionParses(t *testing.T) {
	apps, err := List()
	if err != nil {
		t.Fatalf("a shipped app definition does not parse: %v", err)
	}
	if len(apps) == 0 {
		t.Fatal("the app store ships nothing")
	}
}

// The index is what a client reads before fetching anything, so a name in it
// that has no definition is a store advertising something it cannot serve.
func TestIndex_MatchesTheDefinitionsBesideIt(t *testing.T) {
	index, err := Index()
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	apps, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != len(apps) {
		t.Errorf("the index lists %d apps and the store holds %d", len(index), len(apps))
	}

	byName := map[string]App{}
	for _, a := range apps {
		byName[a.Name] = a
	}
	for _, row := range index {
		app, ok := byName[row.Name]
		if !ok {
			t.Errorf("the index lists %q, which has no definition", row.Name)
			continue
		}
		// The index is a copy of fields that live in the definition, so it is
		// exactly the sort of thing that drifts.
		if row.Version != app.Source.Version {
			t.Errorf("%s: index says version %q, the definition says %q", row.Name, row.Version, app.Source.Version)
		}
		if row.Framework != app.Framework {
			t.Errorf("%s: index says framework %q, the definition says %q", row.Name, row.Framework, app.Framework)
		}
		if row.Label != app.Label {
			t.Errorf("%s: index says label %q, the definition says %q", row.Name, row.Label, app.Label)
		}
	}
}

// Every placeholder a shipped config template names has to be one servlo can
// fill, or the install fails at the last step having already downloaded and
// unpacked a release.
func TestList_EveryConfigTemplateRenders(t *testing.T) {
	apps, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range apps {
		if app.ConfigFile.Template == "" {
			continue
		}
		values := map[string]string{
			"db_name": "site", "db_user": "site_u", "db_password": "generated",
			"db_host": "servlo-mysql", "site_url": "https://example.com",
		}
		secrets, err := app.GenerateSecrets()
		if err != nil {
			t.Fatalf("%s: %v", app.Name, err)
		}
		for k, v := range secrets {
			values[k] = v
		}
		if _, err := app.ConfigFile.Render(values); err != nil {
			t.Errorf("%s: %v", app.Name, err)
		}
	}
}

// An app takes its detection, deploy template, worker set and doctor checks
// from a framework definition, so a framework name with no definition behind it
// is an app that installs and then has none of them. Parsing cannot catch it:
// the field is present and non-empty, and only the store beside it knows the
// name is wrong.
func TestList_EveryAppNamesAFrameworkTheStoreHas(t *testing.T) {
	apps, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range apps {
		entries, err := fs.ReadDir(stores.FS(), "frameworks/"+app.Framework)
		if err != nil || len(entries) == 0 {
			t.Errorf("%s names the framework %q, which the framework store does not have", app.Name, app.Framework)
		}
	}
}

func TestLoad_RefusesANameThatIsAPath(t *testing.T) {
	for _, bad := range []string{"../../etc/passwd", "a/b", "something.yaml"} {
		if _, err := Load(bad); err == nil {
			t.Errorf("Load(%q) was accepted", bad)
		}
	}
}

// A setup form naming a placeholder servlo cannot fill would fail at the very
// last step of an install, with a release already downloaded, a database
// already created and a config file already written.
func TestList_EverySetupFormCanBeFilled(t *testing.T) {
	apps, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range apps {
		if !app.Setup.Declared() {
			continue
		}
		values := map[string]string{
			"site_title": "Example", "admin_user": "admin",
			"admin_password": "generated", "admin_email": "a@example.com",
			"site_url": "https://example.com",
		}
		// A server that is not there, so this exercises the rendering and stops
		// before the request. An unfillable placeholder is reported before the
		// connection is attempted.
		err := app.Setup.Run(t.Context(), "http://127.0.0.1:1", values)
		if err != nil && strings.Contains(err.Error(), "no value for") {
			t.Errorf("%s: %v", app.Name, err)
		}
	}
}
