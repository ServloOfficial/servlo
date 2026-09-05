// Package sitesmtp wires a site's outgoing mail: it writes the site's SMTP
// account into whichever env keys the site's framework declares, and it sends
// the test message that proves the account works.
//
// Which keys carry mail is a framework question, not a servlo one. Laravel has
// MAIL_HOST and MAIL_PASSWORD, Symfony has one MAILER_DSN with everything
// inside it, CodeIgniter addresses its by dotted path and WordPress has neither
// because it has no .env at all. All of them say so in the store, in the same
// place they say which keys carry the database, so no Go here names a
// framework or one of its keys.
package sitesmtp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/envfile"
	"github.com/ServloOfficial/servlo/internal/mailsend"
	"github.com/ServloOfficial/servlo/internal/sitetpl"
)

// declared returns the KEY=VALUE lines the site's framework declares for mail,
// still holding their placeholders.
func declared(site *config.Site) (*config.Framework, []string, error) {
	if site == nil {
		return nil, nil, fmt.Errorf("no site")
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok || !fw.HasEnvConfig() {
		return nil, nil, fmt.Errorf("servlo does not manage an env file for %s, so its mail settings have to be written by hand", site.Name)
	}
	if fw.Env.SMTP == nil || len(fw.Env.SMTP.Vars) == 0 {
		return nil, nil, fmt.Errorf("nothing in the %s definition says which keys carry mail settings, so %s has to be wired by hand", fw.Name, site.Name)
	}
	return fw, fw.Env.SMTP.Vars, nil
}

// updates returns those keys with the values they should now hold. It needs an
// account, because there is nothing to write without one.
func updates(site *config.Site) (map[string]string, error) {
	fw, vars, err := declared(site)
	if err != nil {
		return nil, err
	}
	if _, ok, err := config.SiteSMTP(site.Name); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("%s has no SMTP settings yet", site.Name)
	}

	ctx := sitetpl.ForSite(site)
	out := map[string]string{}
	for _, kv := range vars {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		out[k] = sitetpl.Apply(v, ctx)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the %s definition declares no usable mail keys", fw.Name)
	}
	return out, nil
}

// EnvKeys names the keys a save would write, sorted, so the panel can list them
// before anybody presses the button, including on a site that has no account
// yet. It never returns their values: one of them is the password.
func EnvKeys(site *config.Site) ([]string, error) {
	_, vars, err := declared(site)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(vars))
	for _, kv := range vars {
		if k, _, found := strings.Cut(kv, "="); found {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// WriteEnv writes the site's mail settings into its env file and returns the
// keys it wrote. The file ends up 0600 whatever it was before: it now holds an
// SMTP password, and CLAUDE.md §3.7 has one answer for a file that holds one.
func WriteEnv(site *config.Site) ([]string, error) {
	u, err := updates(site)
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
		err = envfile.ApplyPhpConstUpdates(path, u)
	case "php-array":
		err = envfile.ApplyPhpArrayUpdates(path, u)
	default:
		err = envfile.ApplyUpdates(path, u)
	}
	if err != nil {
		return nil, fmt.Errorf("writing %s: %w", file, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("securing %s: %w", file, err)
	}

	keys := make([]string, 0, len(u))
	for k := range u {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// SendTest sends one message through the site's own account, so the operator
// finds out the settings are wrong here rather than from a customer who never
// got a password reset.
func SendTest(site *config.Site, to string) error {
	if site == nil {
		return fmt.Errorf("no site")
	}
	acct, ok, err := config.SiteSMTP(site.Name)
	if err != nil {
		return err
	}
	if !ok || !acct.Configured() {
		return fmt.Errorf("%s has no SMTP settings yet", site.Name)
	}
	subject := "Servlo test message from " + site.PrimaryDomain()
	body := "This is the test message Servlo sends to prove the SMTP settings for " +
		site.PrimaryDomain() + " work. Nothing else was sent."
	return mailsend.Send(acct, to, subject, body)
}
