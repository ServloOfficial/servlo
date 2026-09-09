package nginx

import (
	"strings"
	"testing"
	"text/template"

	"github.com/ServloOfficial/servlo/internal/config"
)

// Everything an operator types that ends up in a vhost, in one place, so a
// validator that is weaker than the file it protects shows up as a config with
// a directive nobody wrote.
//
// This matters beyond one site. A Developer may edit the sites assigned to
// them, nginx serves every site on the machine from one process, and a
// directive smuggled through a header value or a redirect target is a root
// clause in somebody else's server block. So the guard cannot be "the values we
// thought of are refused", it has to be that nothing which passes validation
// changes the shape of the file.
func FuzzVhostValuesCannotAddDirectives(f *testing.F) {
	for _, seed := range []struct{ header, value, from, to string }{
		{"X-Frame-Options", "DENY", "/old", "/new"},
		{"X-Robots-Tag", "noindex", "/a", "https://example.com/b"},
		{"X-A", "b; root /etc;", "/a", "/b"},
		{"X-A", "b\nroot /etc;", "/a", "/b"},
		{"X-A", `b" ; root /etc; add_header X "`, "/a", "/b"},
		{"X-A", "b", "/a;root /etc;", "/b"},
		{"X-A", "b", "/a", "/b;root /etc;"},
		{"X-A", "b", "/a", "https://x/\nroot /etc;"},
		{"X-A", "b}", "/a", "/b{"},
		{"X-A;root /etc", "b", "/a", "/b"},
	} {
		f.Add(seed.header, seed.value, seed.from, seed.to)
	}

	tmplData, err := GetTemplate("vhost.conf.tmpl")
	if err != nil {
		f.Fatal(err)
	}
	tmpl, err := template.New("vhost").Parse(string(tmplData))
	if err != nil {
		f.Fatal(err)
	}

	// The same shape with values nothing could object to. A rendered vhost that
	// differs from this one in its structure gained that structure from input.
	baseline := shape(f, tmpl, "X-Frame-Options", "DENY", "/old", "/new")

	f.Fuzz(func(t *testing.T, header, value, from, to string) {
		site := config.Site{
			Name: "shop", Domains: []string{"shop.example"}, Path: "/home/u/shop",
			PHPVersion: "8.4", PublicDir: "public",
			ResponseHeaders: []config.ResponseHeader{{Name: header, Value: value}},
			Redirects:       []config.Redirect{{From: from, To: to}},
		}
		// A refusal is the guard doing its job, and says nothing about rendering.
		if err := site.ValidateNginxSettings(); err != nil {
			t.Skip()
		}
		if err := site.ValidateRedirects(); err != nil {
			t.Skip()
		}

		got := shape(t, tmpl, header, value, from, to)
		if got != baseline {
			t.Fatalf("header=%q value=%q from=%q to=%q rendered %v, and a vhost of the same shape with harmless values is %v",
				header, value, from, to, got, baseline)
		}
	})
}

// vhostShape counts what only a directive can change. Braces open and close
// blocks, a semicolon ends a directive, and a newline is where the next one
// starts, so a value that adds any of them added syntax rather than content.
type vhostShape struct{ braces, semicolons, lines int }

func shape(t testing.TB, tmpl *template.Template, header, value, from, to string) vhostShape {
	t.Helper()
	rendered, err := renderVhost(tmpl, VhostData{
		Domain: "shop.example", ServerNames: "shop.example", Path: "/home/u/shop",
		PHPVersion: "8.4", PHPVersionShort: "84", FPMContainer: "servlo-php84-fpm",
		FPMSocket: "/run/servlo/fpm/shop.sock", PublicDir: "public",
		ServloSite: "shop", RequestTimeout: 60,
		ResponseHeaders: []config.ResponseHeader{{Name: header, Value: value}},
		Redirects:       []config.Redirect{{From: from, To: to}},
	})
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	body := string(rendered)
	return vhostShape{
		braces:     strings.Count(body, "{") + strings.Count(body, "}"),
		semicolons: strings.Count(body, ";"),
		lines:      strings.Count(body, "\n"),
	}
}
