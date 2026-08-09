package dbuser

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/envfile"
	"github.com/realrashid/servlo/internal/sitetpl"
)

// Writing a rotated credential back into the site's env file.
//
// Which keys carry the credential is a framework question, not a servlo one:
// Laravel has DB_USERNAME and DB_PASSWORD, WordPress has DB_USER, Symfony has
// one DATABASE_URL with both inside it, and Magento addresses its by dotted
// path. All of them already declare it, because that is how `servlo env` wired
// the credential in the first place. So the keys to rewrite are the ones whose
// declared value asks for {{db_user}} or {{db_password}}, rendered again with
// the new values, and no Go here knows a single framework's key names.

// EnvKeys reports which of a site's env keys carry its database credential,
// with the values they should now hold.
func EnvKeys(site *config.Site) (map[string]string, error) {
	if site == nil {
		return nil, fmt.Errorf("no site")
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok || !fw.HasEnvConfig() {
		return nil, fmt.Errorf("servlo does not manage an env file for %s, so its database credentials have to be updated by hand", site.Name)
	}
	conn, err := dbconn.Named(site.Database)
	if err != nil {
		return nil, err
	}
	ctx := sitetpl.ForSite(site)
	updates := map[string]string{}
	for _, kv := range credentialVars(fw, conn) {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		updates[k] = sitetpl.Apply(v, ctx)
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("nothing in the %s definition says which keys carry the database credentials, so %s has to be updated by hand", fw.Name, site.Name)
	}
	return updates, nil
}

// credentialVars are the declared vars that ask for the site's database
// credential: the framework's unconditional ones, and those of whichever of its
// database services speaks the same dialect as the connection the site is on.
// The dialect match is what keeps a Postgres site from being written its
// framework's MySQL connection string.
func credentialVars(fw *config.Framework, conn dbconn.Connection) []string {
	var out []string
	add := func(vars []string) {
		for _, kv := range vars {
			if strings.Contains(kv, "{{db_user}}") || strings.Contains(kv, "{{db_password}}") {
				out = append(out, kv)
			}
		}
	}
	add(fw.Env.Vars)
	for name, def := range fw.Env.Services {
		if dbconn.DialectForService(name) == conn.Family {
			add(def.Vars)
		}
	}
	return out
}

// WriteEnv rewrites the site's env file with its database credential as it now
// stands, and returns the keys it wrote.
func WriteEnv(site *config.Site) ([]string, error) {
	updates, err := EnvKeys(site)
	if err != nil {
		return nil, err
	}
	fw, _ := config.GetFrameworkForDir(site.Framework, site.Path)
	file, format := fw.Env.Resolve(site.Path)
	// The path comes from a framework definition and is joined onto the site
	// directory; anything that escapes it is refused rather than followed.
	if !filepath.IsLocal(file) {
		return nil, fmt.Errorf("the %s definition names an env file outside the site: %q", fw.Name, file)
	}
	path := filepath.Join(site.Path, file)

	switch format {
	case "php-const":
		err = envfile.ApplyPhpConstUpdates(path, updates)
	case "php-array":
		err = envfile.ApplyPhpArrayUpdates(path, updates)
	default:
		err = envfile.ApplyUpdates(path, updates)
	}
	if err != nil {
		return nil, fmt.Errorf("writing %s: %w", file, err)
	}
	keys := make([]string, 0, len(updates))
	for k := range updates {
		keys = append(keys, k)
	}
	return keys, nil
}
