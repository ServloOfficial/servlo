// Package appstore reads the app definitions in stores/apps.
//
// An app is the fourth way to create a site, alongside a folder, a ZIP and a
// clone: one click fetches the application, creates its database, writes its
// config file and sets up its admin account. Which application, and every
// detail of how, is YAML.
//
// No app's name may appear in Go, the same law frameworks and services live
// under (CLAUDE.md §2). Where a capability has no declarative expression yet
// the answer is a general field named for what it does, never for the app that
// wanted it: config_file with a template, never <that app>_config.
//
// This store has no upstream to copy from, so validation carries more weight
// than it does for the other two. A definition that cannot be verified, or that
// names a path outside the site, is refused at parse time rather than
// discovered halfway through an install on somebody's server.
package appstore

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// App is one application servlo can install.
type App struct {
	Name        string `yaml:"name"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
	// Framework names the entry in stores/frameworks this app maps onto, so
	// detection, the deploy template, the worker set and the doctor checks come
	// from there rather than being restated per app.
	Framework string `yaml:"framework"`
	Source    Source `yaml:"source"`
	// Secrets are values servlo generates and makes available to the config
	// template. A definition names what it needs and how long; nothing here
	// knows what any of them are for.
	Secrets    []Secret   `yaml:"secrets"`
	Database   Database   `yaml:"database"`
	ConfigFile ConfigFile `yaml:"config_file"`
	Setup      Setup      `yaml:"setup"`
}

// Source is where a release comes from, pinned and verifiable.
type Source struct {
	Version string `yaml:"version"`
	URL     string `yaml:"url"`
	SHA256  string `yaml:"sha256"`
	// StripPrefix is the single top-level directory inside the archive, when it
	// has one. Every release archive worth installing is wrapped in a directory
	// named for the project, and extracting that verbatim puts the document
	// root one level below where the vhost looks for it.
	StripPrefix string `yaml:"strip_prefix"`
}

// Secret is one generated value the config template can substitute.
type Secret struct {
	Name   string `yaml:"name"`
	Length int    `yaml:"length"`
}

// Database says what the app needs of one.
type Database struct {
	Required bool   `yaml:"required"`
	Charset  string `yaml:"charset"`
}

// ConfigFile is the file the installer writes into the site, and what goes in
// it.
type ConfigFile struct {
	Path     string `yaml:"path"`
	ModeText string `yaml:"mode"`
	Template string `yaml:"template"`

	mode uint32
}

// Mode is the permission the rendered file is written with.
func (c ConfigFile) Mode() uint32 { return c.mode }

var (
	// appName is a single lowercase token: it becomes a filename in the store
	// and a segment of the URL the client fetches, so it may not name a path.
	appName   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
	sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// placeholder matches {{key}} in a config template.
	placeholder = regexp.MustCompile(`\{\{\s*([a-z0-9_]+)\s*\}\}`)
	// placeholderKey is the name half of one, for validating a declared secret.
	placeholderKey = regexp.MustCompile(`^[a-z0-9_]+$`)
)

// Parse reads a definition and refuses one servlo would not be willing to
// install.
func Parse(data []byte) (App, error) {
	var app App
	if err := yaml.Unmarshal(data, &app); err != nil {
		return App{}, fmt.Errorf("reading the app definition: %w", err)
	}

	if !appName.MatchString(app.Name) {
		return App{}, fmt.Errorf("%q is not a usable app name: use lowercase letters, digits and dashes", app.Name)
	}
	if strings.TrimSpace(app.Label) == "" {
		return App{}, fmt.Errorf("app %q has no label", app.Name)
	}
	if strings.TrimSpace(app.Framework) == "" {
		return App{}, fmt.Errorf("app %q names no framework: an app takes its detection, deploy template and doctor checks from one", app.Name)
	}
	if err := app.Source.validate(app.Name); err != nil {
		return App{}, err
	}
	if err := app.ConfigFile.validate(app.Name); err != nil {
		return App{}, err
	}
	if err := app.Setup.validate(app.Name); err != nil {
		return App{}, err
	}
	seen := map[string]bool{}
	for i, sec := range app.Secrets {
		if !placeholderKey.MatchString(sec.Name) {
			return App{}, fmt.Errorf("app %q secret %d has no usable name: use lowercase letters, digits and underscores", app.Name, i+1)
		}
		if seen[sec.Name] {
			return App{}, fmt.Errorf("app %q declares the secret %q twice", app.Name, sec.Name)
		}
		seen[sec.Name] = true
		// Short enough to be guessable is worse than absent, because it looks
		// like a secret to whoever reads the file.
		if sec.Length < minSecretLength || sec.Length > maxSecretLength {
			return App{}, fmt.Errorf("app %q secret %q has length %d, outside the %d to %d servlo will generate", app.Name, sec.Name, sec.Length, minSecretLength, maxSecretLength)
		}
	}
	return app, nil
}

// minSecretLength and maxSecretLength bound a generated value: long enough that
// it is not worth guessing, short enough that it cannot be used to pad a config
// file into something unreasonable.
const (
	minSecretLength = 32
	maxSecretLength = 128
)

func (s Source) validate(app string) error {
	if strings.TrimSpace(s.Version) == "" {
		return fmt.Errorf("app %q pins no source version: an installer that fetches whatever is current installs a different thing every week", app)
	}
	if strings.TrimSpace(s.URL) == "" {
		return fmt.Errorf("app %q has no source url", app)
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		// The server fetches this. Plain HTTP lets anything on the path swap the
		// release, and the checksum below only helps if the definition that
		// carries it arrived intact.
		return fmt.Errorf("app %q has a source url that is not https: %q", app, s.URL)
	}
	if !sha256Hex.MatchString(strings.ToLower(strings.TrimSpace(s.SHA256))) {
		return fmt.Errorf("app %q has no usable source sha256: an unverified release is a supply-chain hole on a machine serving other people's sites", app)
	}
	return nil
}

func (c *ConfigFile) validate(app string) error {
	// An app with no config file is legitimate; one with a template and no path
	// is a definition that forgot where to put it.
	if c.Path == "" && c.Template == "" {
		return nil
	}
	if c.Path == "" {
		return fmt.Errorf("app %q has a config template but no path to write it to", app)
	}
	if err := safeRelativePath(c.Path); err != nil {
		return fmt.Errorf("app %q config_file: %w", app, err)
	}

	mode := c.ModeText
	if strings.TrimSpace(mode) == "" {
		mode = "0600"
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(mode), 8, 32)
	if err != nil {
		return fmt.Errorf("app %q config_file mode %q is not an octal permission", app, c.ModeText)
	}
	// A config file carrying database credentials that anyone on the box can
	// read makes the shared-user tradeoff worse for no gain.
	if parsed&0o077 != 0 {
		return fmt.Errorf("app %q config_file mode %q is readable beyond its owner, and it holds credentials", app, mode)
	}
	c.mode = uint32(parsed)
	return nil
}

// safeRelativePath refuses anything that would not land inside the site.
func safeRelativePath(p string) error {
	if strings.ContainsRune(p, 0) {
		return fmt.Errorf("path %q contains a NUL byte", p)
	}
	if strings.Contains(p, `\`) {
		return fmt.Errorf("path %q contains a backslash", p)
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return fmt.Errorf("path %q is absolute; it must be inside the site", p)
	}
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("path %q points outside the site directory", p)
	}
	return nil
}

// Render fills the template's placeholders from values.
//
// An unknown placeholder is an error rather than an empty string: it means the
// definition asked for something servlo does not have, and a rendered file
// carrying a literal {{...}} into production is the worse of the two outcomes.
func (c ConfigFile) Render(values map[string]string) (string, error) {
	for key, v := range values {
		// These land inside quotes in a config file the app then executes. A
		// value carrying a quote or a newline would end the string it sits in,
		// and a generated password is exactly the value most likely to.
		if strings.ContainsAny(v, "'\"\\\n\r\x00") {
			return "", fmt.Errorf("the value for %q contains a quote, a backslash or a newline, which would break the config file it is written into", key)
		}
		// An empty credential renders to '' without complaint, which writes a
		// config file that either cannot connect or connects as whoever needs
		// no password. Caught here rather than discovered on a live site: it is
		// the same shape of silent-wrong-value bug as an unfilled placeholder,
		// and it very nearly shipped when the panel wiring passed a connection
		// with no password in it.
		if isCredential(key) && v == "" {
			return "", fmt.Errorf("the value for %q is empty, and a config file with a blank credential in it either cannot connect or connects as whoever needs no password", key)
		}
	}

	var missing []string
	out := placeholder.ReplaceAllStringFunc(c.Template, func(match string) string {
		key := placeholder.FindStringSubmatch(match)[1]
		v, ok := values[key]
		if !ok {
			missing = append(missing, key)
			return match
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("the config template names %s, which servlo has no value for", strings.Join(missing, ", "))
	}
	return out, nil
}

// isCredential reports whether an empty value for key would be a blank
// credential rather than a legitimately absent setting.
func isCredential(key string) bool {
	return strings.Contains(key, "password") || strings.Contains(key, "secret") ||
		strings.HasPrefix(key, "salt_") || strings.Contains(key, "_key")
}
