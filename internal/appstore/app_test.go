package appstore

import (
	"strings"
	"testing"
)

const sampleApp = `
name: example
label: Example CMS
description: A content manager.
framework: example-framework
source:
  version: "6.7.1"
  url: https://example.org/example-6.7.1.zip
  sha256: 0000000000000000000000000000000000000000000000000000000000000000
database:
  required: true
config_file:
  path: config.php
  mode: "0600"
  template: |
    <?php
    define('DB_NAME', '{{db_name}}');
    define('DB_USER', '{{db_user}}');
    define('DB_PASSWORD', '{{db_password}}');
    define('DB_HOST', '{{db_host}}');
    define('SITE_URL', '{{site_url}}');
`

func TestParse_ReadsAWholeDefinition(t *testing.T) {
	app, err := Parse([]byte(sampleApp))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if app.Name != "example" || app.Label != "Example CMS" {
		t.Errorf("identity = %q/%q", app.Name, app.Label)
	}
	// The framework it maps onto is what gives it detection, a deploy template
	// and doctor checks without any of that being restated here.
	if app.Framework != "example-framework" {
		t.Errorf("Framework = %q", app.Framework)
	}
	if app.Source.Version != "6.7.1" {
		t.Errorf("Source.Version = %q", app.Source.Version)
	}
	if app.ConfigFile.Mode() != 0o600 {
		t.Errorf("ConfigFile mode = %o, want 600", app.ConfigFile.Mode())
	}
}

// An installer that fetches an unpinned release and runs it unverified is a
// supply-chain hole on a machine that serves other people's sites.
func TestParse_RefusesADefinitionThatCannotBeVerified(t *testing.T) {
	for _, tc := range []struct{ name, drop, want string }{
		{"no checksum", "  sha256: 0000000000000000000000000000000000000000000000000000000000000000\n", "sha256"},
		{"no version", `  version: "6.7.1"` + "\n", "version"},
		{"no url", "  url: https://example.org/example-6.7.1.zip\n", "has no source url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(strings.Replace(sampleApp, tc.drop, "", 1)))
			if err == nil {
				t.Fatalf("a definition with no %s was accepted", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, does not name %s", err, tc.want)
			}
		})
	}
}

func TestParse_RefusesAChecksumThatIsNotOne(t *testing.T) {
	for _, bad := range []string{"not-hex", "abcd", strings.Repeat("z", 64)} {
		src := strings.Replace(sampleApp,
			"sha256: 0000000000000000000000000000000000000000000000000000000000000000",
			"sha256: "+bad, 1)
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("sha256 %q was accepted", bad)
		}
	}
}

// The URL is fetched by the server. Plain HTTP would let anything on the path
// swap the release for another, and the checksum only helps if the definition
// itself arrived intact.
func TestParse_RefusesASourceThatIsNotHTTPS(t *testing.T) {
	for _, bad := range []string{"http://example.org/x.zip", "file:///etc/passwd", "ftp://example.org/x.zip", "/etc/passwd"} {
		src := strings.Replace(sampleApp, "https://example.org/example-6.7.1.zip", bad, 1)
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("source url %q was accepted", bad)
		}
	}
}

// The config file lands inside the site, and its path comes from a definition
// the operator did not write.
func TestParse_RefusesAConfigPathThatEscapesTheSite(t *testing.T) {
	for _, bad := range []string{"../outside.php", "/etc/passwd", "a/../../b.php", `..\x.php`} {
		src := strings.Replace(sampleApp, "path: config.php", "path: "+bad, 1)
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("config path %q was accepted", bad)
		}
	}
}

// A config file holding database credentials that is world-readable is the
// whole shared-user tradeoff made worse for no reason.
func TestParse_RefusesAConfigFileOthersCanRead(t *testing.T) {
	for _, bad := range []string{"0644", "0666", "0604"} {
		src := strings.Replace(sampleApp, `mode: "0600"`, `mode: "`+bad+`"`, 1)
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("config mode %q was accepted", bad)
		}
	}
}

func TestParse_RefusesADefinitionWithNoName(t *testing.T) {
	if _, err := Parse([]byte(strings.Replace(sampleApp, "name: example\n", "", 1))); err == nil {
		t.Fatal("a nameless definition was accepted")
	}
}

// The name becomes a filename and a URL segment, so it may not name a path.
func TestParse_RefusesANameThatIsNotOne(t *testing.T) {
	for _, bad := range []string{"../evil", "a/b", "Example", "with space", ""} {
		src := strings.Replace(sampleApp, "name: example", "name: "+bad, 1)
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("name %q was accepted", bad)
		}
	}
}

// Substitution is what makes the config file declarative. An unknown
// placeholder is a definition asking for something servlo does not have, and
// leaving it in the rendered file would ship a literal {{...}} into production.
func TestRenderConfig_SubstitutesWhatItKnows(t *testing.T) {
	app, err := Parse([]byte(sampleApp))
	if err != nil {
		t.Fatal(err)
	}

	out, err := app.ConfigFile.Render(map[string]string{
		"db_name": "example", "db_user": "example_u", "db_password": "s3cret",
		"db_host": "servlo-mysql", "site_url": "https://example.com",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"'example'", "'example_u'", "'s3cret'", "'servlo-mysql'", "'https://example.com'"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "{{") {
		t.Errorf("a placeholder survived rendering:\n%s", out)
	}
}

func TestRenderConfig_RefusesAPlaceholderItCannotFill(t *testing.T) {
	src := strings.Replace(sampleApp, "{{site_url}}", "{{something_servlo_does_not_have}}", 1)
	app, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}

	_, err = app.ConfigFile.Render(map[string]string{"db_name": "x", "db_user": "x", "db_password": "x", "db_host": "x"})
	if err == nil {
		t.Fatal("an unknown placeholder rendered without complaint")
	}
	if !strings.Contains(err.Error(), "something_servlo_does_not_have") {
		t.Errorf("error = %q, does not name the placeholder", err)
	}
}

// A generated password reaching a config file through string substitution is
// one quote away from breaking the file it lands in, and PHP is not the only
// target a definition might have.
func TestRenderConfig_RefusesAValueThatWouldBreakOutOfItsQuotes(t *testing.T) {
	app, err := Parse([]byte(sampleApp))
	if err != nil {
		t.Fatal(err)
	}

	_, err = app.ConfigFile.Render(map[string]string{
		"db_name": "x", "db_user": "x", "db_host": "x", "site_url": "x",
		"db_password": `pa'ss`,
	})
	if err == nil {
		t.Fatal("a value carrying a quote was substituted verbatim")
	}
}

// An empty credential renders to ” without complaint, which writes a config
// file that either cannot connect or connects as whoever needs no password.
//
// This very nearly shipped. A first attempt at the panel wiring passed a
// connection carrying a database name and host but no password, because servlo
// has no per-site database user to get one from yet, and the renderer accepted
// it silently. Refusing here is what turned that into a blocker rather than a
// live site with a blank password in its config.
func TestRenderConfig_RefusesABlankCredential(t *testing.T) {
	app, err := Parse([]byte(sampleApp))
	if err != nil {
		t.Fatal(err)
	}

	_, err = app.ConfigFile.Render(map[string]string{
		"db_name": "site", "db_user": "site", "db_host": "servlo-mysql",
		"site_url": "https://example.com", "db_password": "",
	})
	if err == nil {
		t.Fatal("a blank database password rendered without complaint")
	}
	if !strings.Contains(err.Error(), "db_password") {
		t.Errorf("error = %q, does not name the empty credential", err)
	}
}

// A value that is legitimately absent is not a credential and must still
// render, or a definition with an optional setting cannot express one.
func TestRenderConfig_AllowsAnEmptyNonCredential(t *testing.T) {
	config := ConfigFile{Template: "locale = '{{language}}'; pass = '{{db_password}}';"}

	if _, err := config.Render(map[string]string{"language": "", "db_password": "set"}); err != nil {
		t.Errorf("an empty non-credential was refused: %v", err)
	}
}
