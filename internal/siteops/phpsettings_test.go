package siteops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/fpmpool"
	"github.com/ServloOfficial/servlo/internal/podman"
)

// settingsHome gives the test its own config, data and nginx directories, and
// stops the reload and the pool signal from reaching anything real.
// The count of runtime reloads, for the caller that cares whether a save that
// changed nothing restarted a live site.
//
// The home is made short on purpose rather than taken from t.TempDir, which
// names the directory after the test. A pool's socket lives under the data
// directory and a unix socket's path has a hard length limit, so a home named
// after a long test function is a home no site on it can have a pool in, which
// would be the test failing for a reason no install has.
func settingsHome(t *testing.T) *int {
	t.Helper()
	home, err := os.MkdirTemp("", "servlo")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) }) //nolint:errcheck
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	prevReload := nginxReloadFn
	nginxReloadFn = func() error { return nil }
	prevFPM := ReloadFPMPoolsFn
	ReloadFPMPoolsFn = func(string) error { return nil }
	prevRuntime := RestartSiteRuntimeFn
	restarted := 0
	RestartSiteRuntimeFn = func(config.Site) { restarted++ }
	t.Cleanup(func() {
		nginxReloadFn = prevReload
		ReloadFPMPoolsFn = prevFPM
		RestartSiteRuntimeFn = prevRuntime
	})
	return &restarted
}

func settingsSite(t *testing.T) *config.Site {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(filepath.Join(path, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	site := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: path, PHPVersion: "8.4", PublicDir: "public",
	}
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}
	return &site
}

// The acceptance criterion, as far as a session can check it: one field change
// moves every directive that decides whether a larger upload succeeds. An
// upload bigger than the old limit is refused by whichever of the three stayed
// low, so all three have to move on one save.
func TestSetSitePHPSettings_OneFieldRaisesEveryLimitThatCouldRefuseTheUpload(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)

	if err := SetSitePHPSettings(site, PHPSettings{MaxUploadMB: 256}); err != nil {
		t.Fatalf("SetSitePHPSettings: %v", err)
	}

	pool := readPool(t, site)
	for _, want := range []string{
		"php_admin_value[upload_max_filesize] = 256M",
		"php_admin_value[post_max_size] = 256M",
	} {
		if !strings.Contains(pool, want) {
			t.Errorf("the pool is missing %q:\n%s", want, pool)
		}
	}
	if vhost := readVhost(t, site); !strings.Contains(vhost, "client_max_body_size 256m;") {
		t.Errorf("the vhost still refuses the upload before PHP sees it:\n%s", vhost)
	}
}

func TestSetSitePHPSettings_MaxExecutionReachesPHPAndNginx(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)

	if err := SetSitePHPSettings(site, PHPSettings{MaxExecutionSeconds: 600}); err != nil {
		t.Fatal(err)
	}

	if pool := readPool(t, site); !strings.Contains(pool, "php_admin_value[max_execution_time] = 600") {
		t.Errorf("the pool is missing the PHP half:\n%s", pool)
	}
	vhost := readVhost(t, site)
	for _, want := range []string{"fastcgi_read_timeout 600s;", "fastcgi_send_timeout 600s;"} {
		if !strings.Contains(vhost, want) {
			t.Errorf("the vhost is missing %q, so nginx gives up before PHP does:\n%s", want, vhost)
		}
	}
}

func TestSetSitePHPSettings_MemoryLimitReachesThePool(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)

	if err := SetSitePHPSettings(site, PHPSettings{MemoryLimitMB: 512}); err != nil {
		t.Fatal(err)
	}

	if pool := readPool(t, site); !strings.Contains(pool, "php_admin_value[memory_limit] = 512M") {
		t.Errorf("the pool is missing the memory limit:\n%s", pool)
	}
}

// Saved, or the values are live until the next restart and then quietly gone.
func TestSetSitePHPSettings_SurvivesInTheRegistry(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)

	if err := SetSitePHPSettings(site, PHPSettings{MaxUploadMB: 64, MaxExecutionSeconds: 120, MemoryLimitMB: 256}); err != nil {
		t.Fatal(err)
	}

	saved, err := config.FindSiteByDomain("shop.example")
	if err != nil {
		t.Fatalf("finding the saved site: %v", err)
	}
	if saved.MaxUploadMB != 64 || saved.MaxExecutionSeconds != 120 || saved.MemoryLimitMB != 256 {
		t.Errorf("saved %d/%d/%d, want 64/120/256", saved.MaxUploadMB, saved.MaxExecutionSeconds, saved.MemoryLimitMB)
	}
}

// A value out of range would produce a pool FPM refuses to start with, which
// takes down every site sharing the container, so it never reaches the files at
// all and the site keeps what it had.
func TestSetSitePHPSettings_RefusesAnOutOfRangeValueWithoutTouchingTheFiles(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)

	if err := SetSitePHPSettings(site, PHPSettings{MaxUploadMB: 64}); err != nil {
		t.Fatal(err)
	}
	if err := SetSitePHPSettings(site, PHPSettings{MaxUploadMB: 1 << 20}); err == nil {
		t.Fatal("an out-of-range upload limit was accepted")
	}

	if pool := readPool(t, site); !strings.Contains(pool, "= 64M") {
		t.Errorf("the refused save changed the pool anyway:\n%s", pool)
	}
	saved, err := config.FindSiteByDomain("shop.example")
	if err != nil {
		t.Fatal(err)
	}
	if saved.MaxUploadMB != 64 {
		t.Errorf("the registry kept %d, want the previous 64", saved.MaxUploadMB)
	}
	if site.MaxUploadMB != 64 {
		t.Errorf("the caller's site was mutated to %d by a save that failed", site.MaxUploadMB)
	}
}

// Clearing a setting has to remove the directive, not leave the last value in
// place: an operator who empties the field is asking for the default back.
func TestSetSitePHPSettings_ClearingASettingRemovesIt(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)

	if err := SetSitePHPSettings(site, PHPSettings{MaxUploadMB: 256}); err != nil {
		t.Fatal(err)
	}
	if err := SetSitePHPSettings(site, PHPSettings{}); err != nil {
		t.Fatal(err)
	}

	if pool := readPool(t, site); strings.Contains(pool, "upload_max_filesize") {
		t.Errorf("the cleared limit survived in the pool:\n%s", pool)
	}
	if vhost := readVhost(t, site); strings.Contains(vhost, "client_max_body_size") {
		t.Errorf("the cleared limit survived in the vhost:\n%s", vhost)
	}
}

func readPool(t *testing.T, site *config.Site) string {
	t.Helper()
	dir := config.FPMPoolDir(podman.FPMContainerName(*site, site.PHPVersion))
	body, err := os.ReadFile(fpmpool.Path(dir, site.Name))
	if err != nil {
		t.Fatalf("reading the pool: %v", err)
	}
	return string(body)
}

func readVhost(t *testing.T, site *config.Site) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf"))
	if err != nil {
		t.Fatalf("reading the vhost: %v", err)
	}
	return string(body)
}

// The nginx half of the Settings tab lands in the same vhost, through the same
// validated commit.
func TestSetSiteNginxSettings_WritesTheHeadersAndTheCacheWindow(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)

	err := SetSiteNginxSettings(site, NginxSettings{
		StaticCacheDays: 30,
		ResponseHeaders: []config.ResponseHeader{{Name: "X-Frame-Options", Value: "DENY"}},
	})
	if err != nil {
		t.Fatalf("SetSiteNginxSettings: %v", err)
	}

	vhost := readVhost(t, site)
	for _, want := range []string{`add_header X-Frame-Options "DENY" always;`, "expires 30d;"} {
		if !strings.Contains(vhost, want) {
			t.Errorf("the vhost is missing %q:\n%s", want, vhost)
		}
	}

	saved, err := config.FindSiteByDomain("shop.example")
	if err != nil {
		t.Fatal(err)
	}
	if saved.StaticCacheDays != 30 || len(saved.ResponseHeaders) != 1 {
		t.Errorf("saved %d days and %d headers, want 30 and 1", saved.StaticCacheDays, len(saved.ResponseHeaders))
	}
}

// A header nginx would choke on never reaches the vhost, and the site keeps
// what it had rather than losing its config to a typo.
func TestSetSiteNginxSettings_RefusesAHeaderWithoutTouchingTheVhost(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)

	if err := SetSiteNginxSettings(site, NginxSettings{
		ResponseHeaders: []config.ResponseHeader{{Name: "X-Ok", Value: "yes"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetSiteNginxSettings(site, NginxSettings{
		ResponseHeaders: []config.ResponseHeader{{Name: "X-Bad", Value: `a"; deny all; add_header b "`}},
	}); err == nil {
		t.Fatal("a header that would end its own directive was accepted")
	}

	if vhost := readVhost(t, site); !strings.Contains(vhost, `add_header X-Ok "yes" always;`) {
		t.Errorf("the refused save changed the vhost anyway:\n%s", vhost)
	}
}

// Clearing the form removes the block rather than leaving the last one live.
func TestSetSiteNginxSettings_ClearingRemovesWhatItWrote(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)

	if err := SetSiteNginxSettings(site, NginxSettings{
		StaticCacheDays: 30,
		ResponseHeaders: []config.ResponseHeader{{Name: "X-Frame-Options", Value: "DENY"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetSiteNginxSettings(site, NginxSettings{}); err != nil {
		t.Fatal(err)
	}

	vhost := readVhost(t, site)
	for _, unwanted := range []string{"X-Frame-Options", "expires 30d"} {
		if strings.Contains(vhost, unwanted) {
			t.Errorf("%q survived the clear:\n%s", unwanted, vhost)
		}
	}
}

// The toggle lands in the vhost as a permanent redirect from the host the
// operator did not choose.
func TestSetSiteNginxSettings_WritesTheCanonicalRedirect(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)
	site.Domains = []string{"shop.example", "www.shop.example"}
	if err := config.AddSite(*site); err != nil {
		t.Fatal(err)
	}

	if err := SetSiteNginxSettings(site, NginxSettings{CanonicalHost: config.CanonicalApex}); err != nil {
		t.Fatalf("SetSiteNginxSettings: %v", err)
	}

	vhost := readVhost(t, site)
	if !strings.Contains(vhost, `if ($host = "www.shop.example")`) {
		t.Errorf("the vhost does not redirect the www host:\n%s", vhost)
	}
	if !strings.Contains(vhost, "return 301 $scheme://shop.example$request_uri;") {
		t.Errorf("the redirect does not target the apex:\n%s", vhost)
	}
}

// A choice the site cannot honour would 301 every visitor to a name nothing
// answers for, and browsers cache that.
func TestSetSiteNginxSettings_RefusesACanonicalTheSiteCannotHonour(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)
	// A vhost already on disk, so the refusal is shown to leave it alone rather
	// than merely never having written one.
	if err := SetSiteNginxSettings(site, NginxSettings{StaticCacheDays: 7}); err != nil {
		t.Fatal(err)
	}

	err := SetSiteNginxSettings(site, NginxSettings{CanonicalHost: config.CanonicalWWW})

	if err == nil {
		t.Fatal("a www canonical was accepted for a site with no www domain")
	}
	vhost := readVhost(t, site)
	if strings.Contains(vhost, "$host = ") {
		t.Errorf("the refused choice reached the vhost anyway:\n%s", vhost)
	}
	if !strings.Contains(vhost, "expires 7d;") {
		t.Errorf("the refused save discarded the settings the site already had:\n%s", vhost)
	}
	saved, err := config.FindSiteByDomain("shop.example")
	if err != nil {
		t.Fatal(err)
	}
	if saved.CanonicalHost != "" {
		t.Errorf("the registry kept the refused choice: %q", saved.CanonicalHost)
	}
}

// Redirects land in the same vhost through the same validated commit.
func TestSetSiteNginxSettings_WritesTheRedirects(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)

	err := SetSiteNginxSettings(site, NginxSettings{
		RedirectTo:        "https://newshop.example",
		RedirectPermanent: true,
		Redirects:         []config.Redirect{{From: "/old", To: "/new"}},
	})
	if err != nil {
		t.Fatalf("SetSiteNginxSettings: %v", err)
	}

	vhost := readVhost(t, site)
	for _, want := range []string{
		"return 301 https://newshop.example$request_uri;",
		"location = /old {",
		"return 302 /new;",
	} {
		if !strings.Contains(vhost, want) {
			t.Errorf("the vhost is missing %q:\n%s", want, vhost)
		}
	}
}

// A site redirected to itself never arrives, and a permanent one is cached, so
// it is refused before it can reach the config the operator would need to
// reach the panel to undo.
func TestSetSiteNginxSettings_RefusesARedirectLoopWithoutTouchingTheVhost(t *testing.T) {
	settingsHome(t)
	site := settingsSite(t)
	if err := SetSiteNginxSettings(site, NginxSettings{StaticCacheDays: 7}); err != nil {
		t.Fatal(err)
	}

	if err := SetSiteNginxSettings(site, NginxSettings{RedirectTo: "https://shop.example"}); err == nil {
		t.Fatal("a site redirected to a domain it serves was accepted")
	}

	vhost := readVhost(t, site)
	if strings.Contains(vhost, "shop.example$request_uri") {
		t.Errorf("the loop reached the vhost anyway:\n%s", vhost)
	}
	if !strings.Contains(vhost, "expires 7d;") {
		t.Errorf("the refused save discarded what the site already had:\n%s", vhost)
	}
}

// frankenSite is the same site running its own FrankenPHP container rather than
// sharing the FPM one. It has no pool, which is what makes it the interesting
// case: the nginx half of every pair is written for it exactly as before.
func frankenSite(t *testing.T) *config.Site {
	t.Helper()
	path := filepath.Join(t.TempDir(), "octane")
	if err := os.MkdirAll(filepath.Join(path, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	site := config.Site{
		Name: "octane", Domains: []string{"octane.example"},
		Path: path, PHPVersion: "8.4", PublicDir: "public", Runtime: "frankenphp",
	}
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}
	return &site
}

// The same acceptance criterion, on the runtime that has no pool to write it to.
//
// A FrankenPHP site is PHP that servlo builds, runs and mounts config into, so
// there is nothing about it that makes the PHP half of the pair unwritable. The
// pool is simply not where it goes. Before this, raising the upload limit moved
// nginx's client_max_body_size and nothing else, which is the failure the pair
// exists to prevent: the upload passes nginx and PHP refuses it at the default
// the image was built with.
func TestSetSitePHPSettings_FrankenPHPGetsThePHPHalfOfEveryPair(t *testing.T) {
	settingsHome(t)
	site := frankenSite(t)

	if err := SetSitePHPSettings(site, PHPSettings{
		MaxUploadMB: 256, MaxExecutionSeconds: 600, MemoryLimitMB: 512,
	}); err != nil {
		t.Fatalf("SetSitePHPSettings: %v", err)
	}

	ini := readManagedIni(t, site)
	for _, want := range []string{
		"upload_max_filesize = 256M",
		"post_max_size = 256M",
		"max_execution_time = 600",
		"memory_limit = 512M",
	} {
		if !strings.Contains(ini, want) {
			t.Errorf("the site's php.ini is missing %q, so nginx allows what PHP refuses:\n%s", want, ini)
		}
	}
	if vhost := readVhost(t, site); !strings.Contains(vhost, "client_max_body_size 256m;") {
		t.Errorf("the vhost half is missing too:\n%s", vhost)
	}
}

// Clearing a setting has to take the directive away here as well, or the
// operator who empties the field keeps the old ceiling with nothing on screen
// saying so.
func TestSetSitePHPSettings_FrankenPHPClearingASettingRemovesIt(t *testing.T) {
	settingsHome(t)
	site := frankenSite(t)

	if err := SetSitePHPSettings(site, PHPSettings{MaxUploadMB: 256}); err != nil {
		t.Fatal(err)
	}
	if err := SetSitePHPSettings(site, PHPSettings{}); err != nil {
		t.Fatal(err)
	}
	if ini := readManagedIni(t, site); strings.Contains(ini, "upload_max_filesize") {
		t.Errorf("the cleared setting is still in the file:\n%s", ini)
	}
}

func readManagedIni(t *testing.T, site *config.Site) string {
	t.Helper()
	b, err := os.ReadFile(config.SitePHPManagedIniFile(site.Name))
	if err != nil {
		t.Fatalf("reading the site's managed php.ini: %v", err)
	}
	return string(b)
}

// A save that changes nothing must not restart the site. Every vhost
// regeneration passes through here, and a FrankenPHP site whose container went
// down for a couple of seconds on each one would be a site that flickers
// whenever anything unrelated touches it.
func TestSetSitePHPSettings_FrankenPHPDoesNotRestartWhenNothingChanged(t *testing.T) {
	restarted := settingsHome(t)
	site := frankenSite(t)

	if err := SetSitePHPSettings(site, PHPSettings{MaxUploadMB: 256}); err != nil {
		t.Fatal(err)
	}
	after := *restarted
	if after == 0 {
		t.Fatal("the setting was written and the site never picked it up")
	}
	if err := SetSitePHPSettings(site, PHPSettings{MaxUploadMB: 256}); err != nil {
		t.Fatal(err)
	}
	if *restarted != after {
		t.Errorf("a save that changed nothing restarted the site (%d then %d)", after, *restarted)
	}
}
