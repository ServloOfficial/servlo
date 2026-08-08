package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const deployYAML = `
name: example-framework
label: Example
public_dir: public
deploy:
  script: |
    composer install --no-dev
    php bin/console cache:clear
  migrate: php bin/console doctrine:migrations:migrate
  exclude:
    - var/uploads
    - var/plugins
  health: /up
`

func parseFramework(t *testing.T, src string) Framework {
	t.Helper()
	var fw Framework
	if err := yaml.Unmarshal([]byte(src), &fw); err != nil {
		t.Fatalf("parsing the definition: %v", err)
	}
	return fw
}

func TestDeployProfile_ReadsTheWholeBlock(t *testing.T) {
	fw := parseFramework(t, deployYAML)

	if fw.Deploy == nil {
		t.Fatal("the deploy block was not read at all")
	}
	if !strings.Contains(fw.Deploy.Script, "composer install --no-dev") {
		t.Errorf("Script = %q", fw.Deploy.Script)
	}
	if fw.Deploy.Migrate != "php bin/console doctrine:migrations:migrate" {
		t.Errorf("Migrate = %q", fw.Deploy.Migrate)
	}
	if len(fw.Deploy.Exclude) != 2 || fw.Deploy.Exclude[0] != "var/uploads" {
		t.Errorf("Exclude = %v", fw.Deploy.Exclude)
	}
	if fw.Deploy.Health != "/up" {
		t.Errorf("Health = %q", fw.Deploy.Health)
	}
}

// A framework that declares none is the ordinary case: plain PHP has nothing to
// build and nothing to migrate, and its deploy script starts empty.
func TestDeployProfile_AbsentIsFine(t *testing.T) {
	fw := parseFramework(t, "name: plain\nlabel: Plain\npublic_dir: public\n")

	if fw.Deploy != nil {
		t.Errorf("a framework declaring no deploy block got one: %+v", fw.Deploy)
	}
	if got := fw.DeployScript(); got != "" {
		t.Errorf("DeployScript = %q, want empty for a framework with no profile", got)
	}
	if fw.MigrateCommand() != "" || len(fw.DeployExcludes()) != 0 || fw.HealthPath() != "" {
		t.Error("a framework with no profile reported deploy settings anyway")
	}
}

// The migration command is what decides whether a deploy takes a database
// backup first, so the framework has to be able to say what one looks like
// without any Go knowing the answer.
func TestDeployProfile_RecognisesAMigrationInAScript(t *testing.T) {
	fw := parseFramework(t, deployYAML)

	if !fw.ScriptMigrates("php bin/console doctrine:migrations:migrate --no-interaction") {
		t.Error("a script running the declared migration was not recognised as migrating")
	}
	if fw.ScriptMigrates("composer install --no-dev\nnpm ci") {
		t.Error("a script with no migration was treated as one")
	}
	// A commented-out line is not a command. Reading one as a migration takes a
	// database snapshot on every deploy of a script that once had one.
	if fw.ScriptMigrates("# php bin/console doctrine:migrations:migrate") {
		t.Error("a commented-out migration was treated as a live one")
	}
}

// A framework with no migration command cannot be said to migrate, whatever its
// script contains.
func TestDeployProfile_NoMigrationCommandNeverMigrates(t *testing.T) {
	fw := parseFramework(t, "name: plain\nlabel: Plain\npublic_dir: public\n")

	if fw.ScriptMigrates("php bin/console doctrine:migrations:migrate") {
		t.Error("a framework declaring no migration command reported one")
	}
}

// An exclude path is a path inside the site, and it reaches this from a
// definition the operator did not write.
func TestDeployProfile_RefusesAnExcludeThatEscapesTheSite(t *testing.T) {
	for _, bad := range []string{"../outside", "/etc", `..\x`, "a/../../b"} {
		src := strings.Replace(deployYAML, "    - var/uploads", "    - "+bad, 1)
		fw := parseFramework(t, src)
		if err := fw.ValidateDeploy(); err == nil {
			t.Errorf("exclude %q was accepted", bad)
		}
	}
	// YAML drops an empty sequence item rather than passing "" through, so the
	// empty case cannot arrive from a definition and is checked where it can:
	// directly, for a profile built in Go.
	empty := Framework{Name: "x", Deploy: &FrameworkDeploy{Exclude: []string{""}}}
	if err := empty.ValidateDeploy(); err == nil {
		t.Error("an empty exclude path was accepted")
	}
}

// The health path is requested against the site after a deploy.
func TestDeployProfile_RefusesAHealthPathThatIsNotOne(t *testing.T) {
	for _, bad := range []string{"up", "https://elsewhere.example/up", "/up there", ""} {
		src := strings.Replace(deployYAML, "  health: /up", "  health: "+bad, 1)
		fw := parseFramework(t, src)
		if bad == "" {
			// An absent health path is legitimate; only a malformed one is not.
			continue
		}
		if err := fw.ValidateDeploy(); err == nil {
			t.Errorf("health path %q was accepted", bad)
		}
	}
}

// The law this story exists to keep, stated as behaviour rather than as a grep.
//
// Two definitions differing only in their profile must deploy differently, and
// two differing only in their name must deploy identically. That is what "no
// framework name in Go" means operationally: nothing can be reading the name to
// decide, because the name carries no information the profile does not.
func TestDeployProfile_BehaviourFollowsTheProfileNotTheName(t *testing.T) {
	profile := func(name, migrate string) Framework {
		return parseFramework(t, "name: "+name+"\nlabel: X\npublic_dir: public\ndeploy:\n  migrate: "+migrate+"\n")
	}

	// Same name, different profiles: they must disagree.
	a := profile("laravel", "php artisan migrate")
	b := profile("laravel", "php bin/console doctrine:migrations:migrate")
	if !a.ScriptMigrates("php artisan migrate --force") {
		t.Error("a profile did not recognise its own migration")
	}
	if b.ScriptMigrates("php artisan migrate --force") {
		t.Error("a profile recognised another framework's migration, so something is reading the name")
	}

	// Different names, same profile: they must agree.
	c := profile("wordpress", "php artisan migrate")
	if c.ScriptMigrates("php artisan migrate --force") != a.ScriptMigrates("php artisan migrate --force") {
		t.Error("two identical profiles behaved differently, so the name is being read")
	}
	if c.DeployScript() != a.DeployScript() || c.HealthPath() != a.HealthPath() {
		t.Error("two identical profiles reported different settings")
	}
}

// The definitions servlo actually ships, checked against what a deploy of each
// has to do. Written against the store rather than against names in Go: the
// test names them because it is testing the data, which is the one place a
// framework name belongs.
func TestDeployProfile_ShippedDefinitionsAreUsable(t *testing.T) {
	for _, tc := range []struct {
		file          string
		wantsMigrate  bool
		wantsExcludes []string
	}{
		{"laravel/12.yaml", true, nil},
		{"laravel/13.yaml", true, nil},
		{"wordpress/6.yaml", false, []string{"wp-content/uploads", "wp-content/plugins"}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "stores", "frameworks", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			fw := parseFramework(t, string(body))

			if fw.Deploy == nil {
				t.Fatal("ships no deploy profile, so a site on it has nothing to start from")
			}
			if err := fw.ValidateDeploy(); err != nil {
				t.Fatalf("the shipped profile does not validate: %v", err)
			}
			if got := fw.MigrateCommand() != ""; got != tc.wantsMigrate {
				t.Errorf("declares a migration command = %v, want %v", got, tc.wantsMigrate)
			}
			// A framework whose script runs migrations has to declare the
			// command that recognises them, or the pre-deploy backup never
			// fires for the sites that most need it.
			if tc.wantsMigrate && !fw.ScriptMigrates(fw.DeployScript()) {
				t.Errorf("the shipped script's migration is not recognised by the declared command:\nscript=%q\nmigrate=%q", fw.DeployScript(), fw.MigrateCommand())
			}
			for _, want := range tc.wantsExcludes {
				if !slices.Contains(fw.DeployExcludes(), want) {
					t.Errorf("does not protect %s, which the client writes and the repository does not own", want)
				}
			}
			if fw.HealthPath() == "" {
				t.Error("names no health path, so a deploy cannot tell whether the site came back")
			}
		})
	}
}
