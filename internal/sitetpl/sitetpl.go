// Package sitetpl expands the {{…}} placeholders a framework definition may use
// in env vars, setup steps, and commands, so the store can declare a per-site
// value (a base URL, a database name) without servlo knowing the framework.
package sitetpl

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbcred"
	"github.com/ServloOfficial/servlo/internal/grouping"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/serviceops"
)

// Ctx holds the values available to placeholders. An empty field leaves its
// placeholder untouched rather than substituting an empty string.
type Ctx struct {
	Site   string // database / handle name (underscored)
	Bucket string // S3-safe bucket name (lowercase, hyphens)
	Domain string // primary domain (e.g. myapp.test)
	Scheme string // "http" or "https"

	// The site's database connection, which is where a definition gets its
	// coordinates from rather than writing servlo-mysql into every framework.
	// A definition that spells them out still works and still points at the
	// local default; one that uses these follows the site to a managed
	// database without a line of Go knowing the framework's key names.
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string

	// The site's SMTP account, the same idea one layer over: the definition
	// says which keys carry the mail transport, the site says what goes in
	// them. SMTPSet distinguishes "no account configured", which leaves the
	// placeholders alone, from an account whose optional fields are empty,
	// which renders them empty.
	SMTPSet         bool
	SMTPHost        string
	SMTPPort        string
	SMTPUser        string
	SMTPPassword    string
	SMTPFromAddress string
	SMTPFromName    string
	// Three ways to say the same thing, because frameworks do not agree on the
	// word for it: Laravel writes MAIL_ENCRYPTION=tls or the literal null,
	// CodeIgniter and the WordPress mail plugins want a bare tls or nothing at
	// all, and a DSN query parameter wants true or false. A definition picks
	// the one its own key understands.
	SMTPEncryption string // "tls" | "null"
	SMTPCrypto     string // "tls" | ""
	SMTPTLS        string // "true" | "false"
	// The DSN forms. A password with an @ or a colon in it silently truncates a
	// smtp://user:pass@host URL, so a definition that builds one asks for these.
	SMTPUserURLEncoded     string
	SMTPPasswordURLEncoded string
}

// versionedServices are the presets whose {{<name>_version}} placeholder resolves
// to the running container's image version.
var versionedServices = []string{"mysql", "postgres", "redis", "meilisearch"}

// Apply replaces {{site}}, {{site_testing}}, {{bucket}}, {{domain}}, {{scheme}},
// and the {{<service>_version}} placeholders in s.
func Apply(s string, ctx Ctx) string {
	if ctx.Site != "" {
		s = strings.ReplaceAll(s, "{{site}}", ctx.Site)
		s = strings.ReplaceAll(s, "{{site_testing}}", ctx.Site+"_testing")
	}
	if ctx.Bucket != "" {
		s = strings.ReplaceAll(s, "{{bucket}}", ctx.Bucket)
	}
	if ctx.Domain != "" {
		s = strings.ReplaceAll(s, "{{domain}}", ctx.Domain)
	}
	if ctx.Scheme != "" {
		s = strings.ReplaceAll(s, "{{scheme}}", ctx.Scheme)
	}
	for placeholder, value := range map[string]string{
		"{{db_host}}":     ctx.DBHost,
		"{{db_port}}":     ctx.DBPort,
		"{{db_user}}":     ctx.DBUser,
		"{{db_password}}": ctx.DBPassword,
	} {
		if value != "" {
			s = strings.ReplaceAll(s, placeholder, value)
		}
	}
	if ctx.SMTPSet {
		for placeholder, value := range map[string]string{
			"{{smtp_host}}":         ctx.SMTPHost,
			"{{smtp_port}}":         ctx.SMTPPort,
			"{{smtp_user}}":         ctx.SMTPUser,
			"{{smtp_password}}":     ctx.SMTPPassword,
			"{{smtp_encryption}}":   ctx.SMTPEncryption,
			"{{smtp_crypto}}":       ctx.SMTPCrypto,
			"{{smtp_tls}}":          ctx.SMTPTLS,
			"{{smtp_from_address}}": ctx.SMTPFromAddress,
			"{{smtp_from_name}}":    ctx.SMTPFromName,

			"{{smtp_user_urlencoded}}":     ctx.SMTPUserURLEncoded,
			"{{smtp_password_urlencoded}}": ctx.SMTPPasswordURLEncoded,
		} {
			s = strings.ReplaceAll(s, placeholder, value)
		}
	}
	for _, svc := range versionedServices {
		placeholder := "{{" + svc + "_version}}"
		if strings.Contains(s, placeholder) {
			s = strings.ReplaceAll(s, placeholder, podman.ServiceVersion("servlo-"+svc))
		}
	}
	return s
}

// DBName returns the database handle for the project at path: the site's slug,
// or the group main's database when the site is a shared-DB group secondary.
func DBName(path string) string {
	name := filepath.Base(path)
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if s.Path == path {
				if shared, ok := grouping.SharedDBNameFor(&s); ok {
					return shared
				}
				name = s.Name
				break
			}
		}
	}
	return config.SiteSlug(name)
}

// ForSite builds the context for a registered site.
func ForSite(site *config.Site) Ctx {
	if site == nil {
		return Ctx{}
	}
	db := DBName(site.Path)
	scheme := "http"
	if site.Secured {
		scheme = "https"
	}
	ctx := Ctx{
		Site:   db,
		Bucket: serviceops.S3BucketName(db),
		Domain: site.PrimaryDomain(),
		Scheme: scheme,
	}
	// A connection servlo cannot resolve leaves the placeholders alone, the
	// same as every other empty value here. The definition is then written into
	// the env file with {{db_host}} still in it, which is visible, rather than
	// with a host guessed on the site's behalf, which is not.
	if c, err := dbconn.Named(site.Database); err == nil {
		ctx.DBHost = c.Host
		ctx.DBPort = strconv.Itoa(c.Port)
		ctx.DBUser = c.User
		ctx.DBPassword = c.Password
		// The site's own account where it has one. The administrator is the
		// fallback for a site created before per-site accounts existed, which is
		// what it is already using: nothing there breaks, and it moves onto its
		// own account the next time `servlo env` runs.
		if cred, ok := dbcred.For(c, db); ok {
			ctx.DBUser = cred.User
			ctx.DBPassword = cred.Password
		}
	}
	if smtp, ok, err := config.SiteSMTP(site.Name); err == nil && ok {
		ctx.SMTPSet = true
		ctx.SMTPHost = smtp.Host
		ctx.SMTPPort = strconv.Itoa(smtp.Port)
		ctx.SMTPUser = smtp.Username
		ctx.SMTPPassword = smtp.Password
		ctx.SMTPFromAddress = smtp.FromAddress
		ctx.SMTPFromName = smtp.FromName
		encrypted := smtp.Encryption != config.SMTPNone
		ctx.SMTPEncryption = "null"
		ctx.SMTPCrypto = ""
		ctx.SMTPTLS = "false"
		if encrypted {
			ctx.SMTPEncryption, ctx.SMTPCrypto, ctx.SMTPTLS = "tls", "tls", "true"
		}
		ctx.SMTPUserURLEncoded = url.QueryEscape(smtp.Username)
		ctx.SMTPPasswordURLEncoded = url.QueryEscape(smtp.Password)
	}
	return ctx
}

// ForPath builds the context for the project at path, falling back to a
// name-only context when the path is not a registered site.
func ForPath(path string) Ctx {
	if reg, err := config.LoadSites(); err == nil {
		for i := range reg.Sites {
			if reg.Sites[i].Path == path {
				return ForSite(&reg.Sites[i])
			}
		}
	}
	db := DBName(path)
	return Ctx{Site: db, Bucket: serviceops.S3BucketName(db)}
}

// ExpandCommands returns cmds with placeholders expanded in each shell string.
func ExpandCommands(cmds []config.FrameworkCommand, ctx Ctx) []config.FrameworkCommand {
	out := make([]config.FrameworkCommand, len(cmds))
	copy(out, cmds)
	for i := range out {
		out[i].Command = Apply(out[i].Command, ctx)
	}
	return out
}
