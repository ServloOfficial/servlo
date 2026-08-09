// Package sitetpl expands the {{…}} placeholders a framework definition may use
// in env vars, setup steps, and commands, so the store can declare a per-site
// value (a base URL, a database name) without servlo knowing the framework.
package sitetpl

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/grouping"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/serviceops"
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
