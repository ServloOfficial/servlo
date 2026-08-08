package nginx

import (
	"strings"
	"testing"
	"text/template"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/fpmpool"
)

// One field, every place it has to land.
//
// Max upload size is three directives in two files: upload_max_filesize and
// post_max_size in the site's pool, client_max_body_size in its vhost. Raising
// only some of them reads to an operator as the setting not working, because
// whichever half stayed low is the one that refuses the upload. So this renders
// both files from one site and checks all three moved together (CLAUDE.md
// §3.4).
func TestSiteSettings_MaxUploadWritesPHPAndNginxTogether(t *testing.T) {
	site := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: "/home/u/shop", PHPVersion: "8.4", PublicDir: "public",
		MaxUploadMB: 256,
	}

	pool, err := fpmpool.Render(fpmpool.Settings{
		Site: site.Name, Root: site.Path, SocketDir: "/run/servlo/fpm",
		MaxUploadMB: site.MaxUploadMB,
	})
	if err != nil {
		t.Fatalf("Render pool: %v", err)
	}
	for _, want := range []string{
		"php_admin_value[upload_max_filesize] = 256M",
		"php_admin_value[post_max_size] = 256M",
	} {
		if !strings.Contains(pool, want) {
			t.Errorf("the pool is missing %q:\n%s", want, pool)
		}
	}

	for _, tmpl := range []string{"vhost.conf.tmpl", "vhost-ssl.conf.tmpl"} {
		out := renderTemplate(t, tmpl, vhostDataFor(site))
		if !strings.Contains(out, "client_max_body_size 256m;") {
			t.Errorf("%s does not raise nginx's own limit, so the upload is refused before PHP sees it:\n%s", tmpl, out)
		}
	}
}

// A site that sets nothing gets nginx's default, not a directive restating it,
// so the global config can still move it.
func TestSiteSettings_UnsetUploadWritesNoNginxDirective(t *testing.T) {
	site := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: "/home/u/shop", PHPVersion: "8.4", PublicDir: "public",
	}

	for _, tmpl := range []string{"vhost.conf.tmpl", "vhost-ssl.conf.tmpl"} {
		out := renderTemplate(t, tmpl, vhostDataFor(site))
		if strings.Contains(out, "client_max_body_size") {
			t.Errorf("%s wrote a limit the site never set:\n%s", tmpl, out)
		}
	}
}

// Max execution time is the same shape: PHP gives up after max_execution_time,
// nginx gives up after its fastcgi timeouts, and whichever is lower is the one
// the visitor experiences. A long-running import set to 600 in PHP still dies
// at nginx's 60.
func TestSiteSettings_MaxExecutionWritesPHPAndNginxTogether(t *testing.T) {
	site := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: "/home/u/shop", PHPVersion: "8.4", PublicDir: "public",
		MaxExecutionSeconds: 600,
	}

	pool, err := fpmpool.Render(fpmpool.Settings{
		Site: site.Name, Root: site.Path, SocketDir: "/run/servlo/fpm",
		MaxExecutionSeconds: site.MaxExecutionSeconds,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pool, "php_admin_value[max_execution_time] = 600") {
		t.Errorf("the pool is missing the PHP half:\n%s", pool)
	}

	for _, tmpl := range []string{"vhost.conf.tmpl", "vhost-ssl.conf.tmpl"} {
		out := renderTemplate(t, tmpl, vhostDataFor(site))
		for _, want := range []string{
			"fastcgi_read_timeout 600s;",
			"fastcgi_send_timeout 600s;",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s is missing %q, so nginx gives up before PHP does:\n%s", tmpl, want, out)
			}
		}
	}
}

// The site's own execution time is the more specific setting, so it wins over
// the global request timeout rather than being averaged with it or ignored.
func TestSiteSettings_MaxExecutionBeatsTheGlobalRequestTimeout(t *testing.T) {
	site := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: "/home/u/shop", PHPVersion: "8.4", PublicDir: "public",
		MaxExecutionSeconds: 300,
	}

	if got := siteRequestTimeout(site, 60); got != 300 {
		t.Errorf("request timeout = %d, want the site's 300", got)
	}
	// And a site that sets none keeps whatever the global resolution produced.
	site.MaxExecutionSeconds = 0
	if got := siteRequestTimeout(site, 60); got != 60 {
		t.Errorf("request timeout = %d, want the global 60", got)
	}
}

// A value reaching the vhost from the panel decides an nginx directive, so one
// carrying syntax of its own, or one nginx would reject outright, is refused
// rather than written into a config that then fails to load for every site on
// the machine.
func TestSiteSettings_RefusesAnUploadLimitNginxWouldReject(t *testing.T) {
	for _, bad := range []int{-1, 1 << 20} {
		site := config.Site{
			Name: "shop", Domains: []string{"shop.example"},
			Path: "/home/u/shop", PHPVersion: "8.4", PublicDir: "public",
			MaxUploadMB: bad,
		}
		if err := site.ValidatePHPSettings(); err == nil {
			t.Errorf("upload limit %d was accepted", bad)
		}
	}
	for _, bad := range []int{-1, 1 << 20} {
		site := config.Site{Name: "shop", MaxExecutionSeconds: bad}
		if err := site.ValidatePHPSettings(); err == nil {
			t.Errorf("execution time %d was accepted", bad)
		}
	}
	for _, bad := range []int{-1, 1 << 20} {
		site := config.Site{Name: "shop", MemoryLimitMB: bad}
		if err := site.ValidatePHPSettings(); err == nil {
			t.Errorf("memory limit %d was accepted", bad)
		}
	}
}

// vhostDataFor builds the template data the way the generators do, so these
// assertions are about the file nginx reads.
func vhostDataFor(site config.Site) VhostData {
	return VhostData{
		Domain:         site.PrimaryDomain(),
		ServerNames:    site.PrimaryDomain(),
		Path:           site.Path,
		PHPVersion:     site.PHPVersion,
		FPMContainer:   "servlo-php84-fpm",
		PublicDir:      site.PublicDir,
		MaxUploadMB:    site.MaxUploadMB,
		RequestTimeout: siteRequestTimeout(site, 60),
	}
}

func renderTemplate(t *testing.T, name string, data VhostData) string {
	t.Helper()
	raw, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New(name).Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderVhost(tmpl, data)
	if err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	return string(out)
}
