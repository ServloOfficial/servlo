package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// ── Logs field on built-in Laravel ───────────────────────────────────────────

func TestLaravelBuiltinHasLogs(t *testing.T) {
	if len(laravelFramework.Logs) == 0 {
		t.Fatal("built-in Laravel should have Logs configured")
	}
	if laravelFramework.Logs[0].Path != "storage/logs/*.log" {
		t.Errorf("expected storage/logs/*.log, got %s", laravelFramework.Logs[0].Path)
	}
	if laravelFramework.Logs[0].Format != "monolog" {
		t.Errorf("expected monolog format, got %s", laravelFramework.Logs[0].Format)
	}
}

func TestGetFrameworkLaravel_BuiltinLogs(t *testing.T) {
	setConfigDir(t)

	fw, ok := GetFramework("laravel")
	if !ok {
		t.Fatal("expected to find laravel framework")
	}
	if len(fw.Logs) == 0 {
		t.Fatal("GetFramework(laravel) should include built-in Logs")
	}
	if fw.Logs[0].Format != "monolog" {
		t.Errorf("expected monolog, got %s", fw.Logs[0].Format)
	}
}

func TestGetFrameworkLaravel_UserOverridesLogs(t *testing.T) {
	setConfigDir(t)

	// Write a user laravel.yaml that overrides logs
	dir := FrameworksDir()
	os.MkdirAll(dir, 0755)

	userFw := Framework{
		Name: "laravel",
		Logs: []FrameworkLogSource{
			{Path: "storage/logs/*.log", Format: "monolog"},
			{Path: "storage/logs/custom/*.log", Format: "monolog"},
		},
	}
	data, _ := yaml.Marshal(userFw)
	os.WriteFile(filepath.Join(dir, "laravel.yaml"), data, 0644)

	fw, ok := GetFramework("laravel")
	if !ok {
		t.Fatal("expected to find laravel")
	}
	if len(fw.Logs) != 2 {
		t.Fatalf("expected 2 log sources from user override, got %d", len(fw.Logs))
	}
	if fw.Logs[1].Path != "storage/logs/custom/*.log" {
		t.Errorf("second log source path = %q", fw.Logs[1].Path)
	}
}

func TestGetFrameworkLaravel_NoUserOverrideKeepsBuiltinLogs(t *testing.T) {
	setConfigDir(t)

	// Write a user laravel.yaml with only workers, no logs
	dir := FrameworksDir()
	os.MkdirAll(dir, 0755)

	userFw := Framework{
		Name: "laravel",
		Workers: map[string]FrameworkWorker{
			"horizon": {Label: "Horizon", Command: "php artisan horizon"},
		},
	}
	data, _ := yaml.Marshal(userFw)
	os.WriteFile(filepath.Join(dir, "laravel.yaml"), data, 0644)

	fw, ok := GetFramework("laravel")
	if !ok {
		t.Fatal("expected to find laravel")
	}
	// Built-in logs should remain since user didn't override
	if len(fw.Logs) != 1 {
		t.Fatalf("expected 1 built-in log source, got %d", len(fw.Logs))
	}
	if fw.Logs[0].Path != "storage/logs/*.log" {
		t.Errorf("expected built-in log path, got %s", fw.Logs[0].Path)
	}
}

// ── Custom framework with Logs ───────────────────────────────────────────────

func TestGetFrameworkCustom_WithLogs(t *testing.T) {
	setConfigDir(t)

	dir := FrameworksDir()
	os.MkdirAll(dir, 0755)

	fw := Framework{
		Name:      "symfony",
		Label:     "Symfony",
		PublicDir: "public",
		Detect:    []FrameworkRule{{File: "symfony.lock"}},
		Logs: []FrameworkLogSource{
			{Path: "var/log/*.log", Format: "raw"},
		},
	}
	data, _ := yaml.Marshal(fw)
	os.WriteFile(filepath.Join(dir, "symfony.yaml"), data, 0644)

	got, ok := GetFramework("symfony")
	if !ok {
		t.Fatal("expected to find symfony")
	}
	if len(got.Logs) != 1 {
		t.Fatalf("expected 1 log source, got %d", len(got.Logs))
	}
	if got.Logs[0].Path != "var/log/*.log" {
		t.Errorf("log path = %q", got.Logs[0].Path)
	}
	if got.Logs[0].Format != "raw" {
		t.Errorf("log format = %q", got.Logs[0].Format)
	}
}

// The built-in Symfony def must follow Symfony's env convention: servlo writes its
// connection values into .env.local (the gitignored local override), seeded from
// the committed .env. Keeping the built-in aligned with the store def stops the
// two from contradicting each other on the offline fallback path.
func TestGetFrameworkSymfony_BuiltinEnvTargetsEnvLocal(t *testing.T) {
	setConfigDir(t)

	fw, ok := GetFramework("symfony")
	if !ok {
		t.Fatal("GetFramework(symfony): not found")
	}
	if fw.Env.File != ".env.local" {
		t.Errorf("Env.File = %q, want .env.local", fw.Env.File)
	}
	if fw.Env.ExampleFile != ".env" {
		t.Errorf("Env.ExampleFile = %q, want .env", fw.Env.ExampleFile)
	}
	if fw.Env.URLKey != "DEFAULT_URI" {
		t.Errorf("Env.URLKey = %q, want DEFAULT_URI", fw.Env.URLKey)
	}
}

func TestGetFrameworkCustom_WithoutLogs(t *testing.T) {
	setConfigDir(t)

	dir := FrameworksDir()
	os.MkdirAll(dir, 0755)

	fw := Framework{
		Name:      "wordpress",
		Label:     "WordPress",
		PublicDir: ".",
		Detect:    []FrameworkRule{{File: "wp-login.php"}},
	}
	data, _ := yaml.Marshal(fw)
	os.WriteFile(filepath.Join(dir, "wordpress.yaml"), data, 0644)

	got, ok := GetFramework("wordpress")
	if !ok {
		t.Fatal("expected to find wordpress")
	}
	if len(got.Logs) != 0 {
		t.Errorf("expected 0 log sources for wordpress, got %d", len(got.Logs))
	}
}

// ── GetFrameworkOrFetch ──────────────────────────────────────────────────────

// A framework the store publishes but the machine hasn't installed must be
// fetched on demand, so `servlo new --framework=X` can scaffold a project type
// you've never built before.
func TestGetFrameworkOrFetch_FetchesUninstalled(t *testing.T) {
	setConfigDir(t)

	prev := frameworkFetchHook
	t.Cleanup(func() { frameworkFetchHook = prev })

	called := ""
	frameworkFetchHook = func(name, version string) (*Framework, error) {
		called = name
		fw := &Framework{Name: name, Label: "CodeIgniter", Version: "4", PublicDir: "public"}
		if err := SaveStoreFramework(fw); err != nil {
			return nil, err
		}
		return fw, nil
	}

	got, ok := GetFrameworkOrFetch("codeigniter")
	if !ok {
		t.Fatal("GetFrameworkOrFetch(codeigniter): not found after fetch")
	}
	if got.Label != "CodeIgniter" {
		t.Errorf("Label = %q, want CodeIgniter", got.Label)
	}
	if called != "codeigniter" {
		t.Errorf("fetch hook called with %q, want codeigniter", called)
	}
}

// An installed framework resolves locally and must never reach for the store.
func TestGetFrameworkOrFetch_SkipsFetchWhenInstalled(t *testing.T) {
	setConfigDir(t)

	prev := frameworkFetchHook
	t.Cleanup(func() { frameworkFetchHook = prev })
	frameworkFetchHook = func(name, version string) (*Framework, error) {
		t.Fatalf("fetch hook called for an installed framework (%q)", name)
		return nil, nil
	}

	if _, ok := GetFrameworkOrFetch("laravel"); !ok {
		t.Fatal("GetFrameworkOrFetch(laravel): built-in not found")
	}
}

// A name the store doesn't publish keeps the original not-found result.
func TestGetFrameworkOrFetch_UnknownStaysNotFound(t *testing.T) {
	setConfigDir(t)

	prev := frameworkFetchHook
	t.Cleanup(func() { frameworkFetchHook = prev })
	frameworkFetchHook = func(name, version string) (*Framework, error) {
		return nil, os.ErrNotExist
	}

	if _, ok := GetFrameworkOrFetch("nonexistent"); ok {
		t.Error("GetFrameworkOrFetch(nonexistent) = true, want false")
	}
}

// ── SaveFramework preserves Logs ─────────────────────────────────────────────

func TestSaveFrameworkLaravel_PersistsLogs(t *testing.T) {
	setConfigDir(t)

	fw := &Framework{
		Name: "laravel",
		Workers: map[string]FrameworkWorker{
			"horizon": {Label: "Horizon", Command: "php artisan horizon"},
		},
		Logs: []FrameworkLogSource{
			{Path: "storage/logs/*.log", Format: "monolog"},
			{Path: "storage/logs/jobs/*.log", Format: "monolog"},
		},
	}
	if err := SaveFramework(fw); err != nil {
		t.Fatal(err)
	}

	// Read back raw YAML to verify logs are persisted
	path := filepath.Join(FrameworksDir(), "laravel.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Framework
	if err := yaml.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Logs) != 2 {
		t.Fatalf("expected 2 log sources saved, got %d", len(saved.Logs))
	}
}

func TestSaveFrameworkCustom_PersistsLogs(t *testing.T) {
	setConfigDir(t)

	fw := &Framework{
		Name:      "drupal",
		Label:     "Drupal",
		PublicDir: "web",
		Logs: []FrameworkLogSource{
			{Path: "sites/default/files/logs/*.log"},
		},
	}
	if err := SaveFramework(fw); err != nil {
		t.Fatal(err)
	}

	got, ok := GetFramework("drupal")
	if !ok {
		t.Fatal("expected to find drupal after save")
	}
	if len(got.Logs) != 1 {
		t.Fatalf("expected 1 log source, got %d", len(got.Logs))
	}
}

// ── ListFrameworks includes Logs ─────────────────────────────────────────────

func TestListFrameworks_IncludesLogs(t *testing.T) {
	setConfigDir(t)

	frameworks := ListFrameworks()
	// At minimum the built-in Laravel
	found := false
	for _, fw := range frameworks {
		if fw.Name == "laravel" {
			found = true
			if len(fw.Logs) == 0 {
				t.Error("ListFrameworks: laravel should have Logs")
			}
		}
	}
	if !found {
		t.Error("ListFrameworks should include laravel")
	}
}

// ── RemoveFramework ─────────────────────────────────────────────────────────

func TestRemoveFramework_UserDefined(t *testing.T) {
	setConfigDir(t)

	dir := FrameworksDir()
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "myfw.yaml"), []byte("name: myfw\n"), 0644)

	if err := RemoveFramework("myfw"); err != nil {
		t.Fatalf("RemoveFramework(user): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "myfw.yaml")); !os.IsNotExist(err) {
		t.Error("expected user file to be removed")
	}
}

func TestRemoveFramework_StoreInstalled(t *testing.T) {
	setConfigDir(t)

	dir := StoreFrameworksDir()
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "symfony.yaml"), []byte("name: symfony\n"), 0644)
	os.WriteFile(filepath.Join(dir, "symfony@7.yaml"), []byte("name: symfony\nversion: \"7\"\n"), 0644)

	if err := RemoveFramework("symfony"); err != nil {
		t.Fatalf("RemoveFramework(store): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "symfony.yaml")); !os.IsNotExist(err) {
		t.Error("expected unversioned store file to be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "symfony@7.yaml")); !os.IsNotExist(err) {
		t.Error("expected versioned store file to be removed")
	}
}

func TestRemoveFramework_NotFound(t *testing.T) {
	setConfigDir(t)

	err := RemoveFramework("nonexistent")
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got: %v", err)
	}
}

// ── FrameworkLogSource YAML round-trip ────────────────────────────────────────

func TestFrameworkLogSource_YAMLRoundTrip(t *testing.T) {
	original := []FrameworkLogSource{
		{Path: "storage/logs/*.log", Format: "monolog"},
		{Path: "var/log/*.log", Format: "raw"},
		{Path: "logs/*.txt"}, // no format
	}

	data, err := yaml.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var loaded []FrameworkLogSource
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 3 {
		t.Fatalf("expected 3, got %d", len(loaded))
	}
	if loaded[0].Path != "storage/logs/*.log" || loaded[0].Format != "monolog" {
		t.Errorf("entry 0: %+v", loaded[0])
	}
	if loaded[1].Format != "raw" {
		t.Errorf("entry 1 format: %q", loaded[1].Format)
	}
	if loaded[2].Format != "" {
		t.Errorf("entry 2 format should be empty, got %q", loaded[2].Format)
	}
}

// ValidatePublicDir guards the nginx document root from a hostile .servlo.yaml
// whose public_dir points outside the project, e.g. ../../etc.
func TestValidatePublicDir(t *testing.T) {
	good := []string{"", ".", "public", "web", "public_html", "src/public"}
	for _, s := range good {
		if err := ValidatePublicDir(s); err != nil {
			t.Errorf("ValidatePublicDir(%q) = %v, want nil", s, err)
		}
	}
	bad := []string{
		"..",
		"../etc",
		"../../etc",
		"public/../etc",
		"public/..",
		"/etc",
		"/etc/passwd",
		"~/.ssh",
		"public\x00evil",
	}
	for _, s := range bad {
		if err := ValidatePublicDir(s); err == nil {
			t.Errorf("ValidatePublicDir(%q) = nil, want error", s)
		}
	}
}

// ── SitesUsingFramework / InstalledFrameworkNames ────────────────────────────

func TestSitesUsingFramework(t *testing.T) {
	setConfigDir(t)

	reg := &SiteRegistry{Sites: []Site{
		{Name: "shop", Framework: "laravel"},
		{Name: "blog", Framework: "wordpress"},
		{Name: "api", Framework: "laravel"},
		{Name: "static", Framework: ""},
	}}
	if err := SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	got := SitesUsingFramework("laravel")
	if len(got) != 2 || got[0] != "shop" || got[1] != "api" {
		t.Errorf("SitesUsingFramework(laravel) = %v, want [shop api]", got)
	}
	if got := SitesUsingFramework("symfony"); len(got) != 0 {
		t.Errorf("SitesUsingFramework(symfony) = %v, want empty", got)
	}
}

func TestSitesUsingFramework_IgnoresUnlinkedParkedEntries(t *testing.T) {
	setConfigDir(t)

	reg := &SiteRegistry{Sites: []Site{
		{Name: "served", Framework: "wordpress"},
		{Name: "parked", Framework: "wordpress", Ignored: true},
	}}
	if err := SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	got := SitesUsingFramework("wordpress")
	if len(got) != 1 || got[0] != "served" {
		t.Errorf("SitesUsingFramework(wordpress) = %v, want [served]", got)
	}
}

func TestInstalledFrameworkNames_ExcludesBuiltins(t *testing.T) {
	setConfigDir(t)

	user := FrameworksDir()
	os.MkdirAll(user, 0755)
	os.WriteFile(filepath.Join(user, "custom.yaml"), []byte("name: custom\n"), 0644)
	os.WriteFile(filepath.Join(user, "laravel.yaml"), []byte("name: laravel\n"), 0644)

	store := StoreFrameworksDir()
	os.MkdirAll(store, 0755)
	os.WriteFile(filepath.Join(store, "symfony.yaml"), []byte("name: symfony\n"), 0644)
	os.WriteFile(filepath.Join(store, "symfony@7.yaml"), []byte("name: symfony\nversion: \"7\"\n"), 0644)
	os.WriteFile(filepath.Join(store, "wordpress@6.yaml"), []byte("name: wordpress\n"), 0644)

	got := InstalledFrameworkNames()
	want := []string{"custom", "wordpress"}
	if len(got) != len(want) {
		t.Fatalf("InstalledFrameworkNames() = %v, want %v (built-ins laravel/symfony must be excluded)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("InstalledFrameworkNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestUnusedInstalledFrameworks(t *testing.T) {
	setConfigDir(t)

	store := StoreFrameworksDir()
	os.MkdirAll(store, 0755)
	os.WriteFile(filepath.Join(store, "wordpress.yaml"), []byte("name: wordpress\n"), 0644)
	os.WriteFile(filepath.Join(store, "drupal.yaml"), []byte("name: drupal\n"), 0644)

	reg := &SiteRegistry{Sites: []Site{
		{Name: "blog", Framework: "wordpress"},
		{Name: "old", Framework: "drupal", Ignored: true},
	}}
	if err := SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	got := UnusedInstalledFrameworks()
	if len(got) != 1 || got[0] != "drupal" {
		t.Errorf("UnusedInstalledFrameworks() = %v, want [drupal]", got)
	}
}

func TestListFrameworkFiles_IncludesUserVersioned(t *testing.T) {
	setConfigDir(t)

	user := FrameworksDir()
	os.MkdirAll(user, 0755)
	versioned := filepath.Join(user, "drupal@10.yaml")
	os.WriteFile(versioned, []byte("name: drupal\nversion: \"10\"\n"), 0644)

	files := ListFrameworkFiles("drupal")
	if len(files) != 1 || files[0].Path != versioned || files[0].Version != "10" {
		t.Fatalf("ListFrameworkFiles(drupal) = %+v, want the user-dir drupal@10 file", files)
	}

	if err := RemoveFramework("drupal"); err != nil {
		t.Fatalf("RemoveFramework: %v", err)
	}
	if _, err := os.Stat(versioned); !os.IsNotExist(err) {
		t.Error("expected user-dir versioned file to be removed")
	}
}

// Every binary ships every definition it was built with, and CLAUDE.md §4 calls
// that embedded copy the floor an install bootstraps from. A site whose version
// cannot be read has no versioned definition to load and nothing to fetch one
// by, and it was falling past that floor to a built-in adapter that predates the
// store: a name, a public directory and a detect rule, with no deploy profile at
// all. What that costs is the pre-deploy database backup, since the marker a
// migration is recognised by lives in the profile.
func TestGetFrameworkForDir_FallsBackToTheEmbeddedStoreNotABareAdapter(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))

	// An application servlo recognises by its marker file, carrying nothing
	// that says which version it is: no composer.json, no .servlo.yaml.
	site := filepath.Join(dir, "site")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "artisan"), []byte("#!/usr/bin/env php\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fw, ok := GetFrameworkForDir("laravel", site)
	if !ok {
		t.Fatal("the framework did not resolve at all")
	}
	if len(fw.MigrateCommands()) == 0 {
		t.Errorf("%s resolved with no migration marker, so no deploy on this site will ever take "+
			"the database backup that is supposed to precede a migration", fw.Label)
	}
	if fw.DeployScript() == "" {
		t.Errorf("%s resolved with no deploy script template", fw.Label)
	}
}

// The definition that comes back for an unreadable version is borrowed: it is
// the newest one shipped, and the project is some other version. So its PHP
// range must not clamp the project, the same rule a definition borrowed from a
// known-but-older version already follows. Without this a site running 8.1
// whose version could not be read would be bumped to the newest definition's
// minimum on every snapshot rebuild.
func TestGetFrameworkForDir_TheEmbeddedFallbackIsMarkedBorrowed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))

	site := filepath.Join(dir, "site")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "artisan"), []byte("#!/usr/bin/env php\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fw, ok := GetFrameworkForDir("laravel", site)
	if !ok {
		t.Fatal("the framework did not resolve at all")
	}
	if !fw.VersionGuessed {
		t.Error("a definition borrowed for a version nobody could read was returned as if it were the " +
			"project's own, so its PHP range will clamp a project it was never written for")
	}
}

// Every framework the binary ships has to resolve from a bare project directory,
// with the profile that carries its guarantees. A site whose version cannot be
// read is the ordinary way to land here: no composer.json, or a constraint like
// dev-main with no number in it.
//
// What rides on the profile is not cosmetic. The migration marker is what
// decides whether a deploy takes a database backup first (§3.5), and the exclude
// list is what stops a deploy removing a client's uploads. Before the embedded
// store became the floor this resolution stands on, two of these came back as a
// bare adapter with neither, and the other nine came back as nothing at all.
func TestGetFrameworkForDir_EveryEmbeddedFrameworkResolvesWithItsProfile(t *testing.T) {
	// One marker file each, from the framework's own detect rules.
	markers := map[string]string{
		"laravel": "artisan", "symfony": "bin/console", "wordpress": "wp-settings.php",
		"drupal": "core/lib/Drupal.php", "magento": "bin/magento", "cakephp": "bin/cake",
		"codeigniter": "spark", "statamic": "please", "joomla": "configuration.php",
		"grav": "bin/grav", "tempest": "tempest",
	}
	// The two that say plainly they have no migration of their own: their schema
	// changes come from a plugin or a core update, which servlo does not drive.
	noMigration := map[string]bool{"joomla": true, "grav": true, "wordpress": true}

	for name, marker := range markers {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))
			site := filepath.Join(dir, "site")
			if err := os.MkdirAll(filepath.Join(site, filepath.Dir(marker)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(site, marker), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}

			fw, ok := GetFrameworkForDir(name, site)
			if !ok {
				t.Fatalf("%s did not resolve at all, so this site has no workers, no doctor checks, "+
					"no deploy template and no migration marker", name)
			}
			if !noMigration[name] && len(fw.MigrateCommands()) == 0 {
				t.Errorf("%s resolved with no migration marker, so no deploy on this site would ever take "+
					"the database backup that is supposed to precede a migration", name)
			}
		})
	}
}
