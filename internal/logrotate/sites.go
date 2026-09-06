package logrotate

import (
	"fmt"

	"github.com/ServloOfficial/servlo/internal/config"
)

// Everything above this line is mechanism. This is where it meets the site
// registry and the framework store: which logs a site has is the framework's
// declaration, and how much of them to keep is the operator's setting.

// SitePolicy is the policy in force, from the global config.
func SitePolicy() Policy {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return DefaultPolicy
	}
	return Policy{
		MaxSizeMB: cfg.Logs.MaxSizeMB,
		Keep:      cfg.Logs.Keep,
		Compress:  !cfg.Logs.KeepUncompressed,
	}.Resolved()
}

// Enabled reports whether servlo rotates at all.
func Enabled() bool {
	cfg, err := config.LoadGlobal()
	return err != nil || !cfg.Logs.Disabled
}

// Globs are the log paths a site's framework declares, relative to the site
// root. Servlo knows nothing here about where any framework keeps its logs; a
// site whose framework declares none has nothing rotated, which is correct
// rather than a gap.
func Globs(site *config.Site) []string {
	if site == nil || site.Framework == "" {
		return nil
	}
	fw, ok := config.GetFramework(site.Framework)
	if !ok {
		return nil
	}
	var out []string
	for _, src := range fw.Logs {
		if src.Path != "" {
			out = append(out, src.Path)
		}
	}
	return out
}

// SiteResult is one site's rotation.
type SiteResult struct {
	Site string
	Result
	Err error
}

// RotateAll rotates every site's logs.
//
// A site that fails does not stop the others. One site with a broken
// permission is not a reason for every other site's logs to keep growing, and
// the failure is reported rather than swallowed.
func RotateAll() ([]SiteResult, error) {
	reg, err := config.LoadSites()
	if err != nil {
		return nil, fmt.Errorf("reading the sites: %w", err)
	}
	policy := SitePolicy()

	var out []SiteResult
	for i := range reg.Sites {
		site := &reg.Sites[i]
		globs := Globs(site)
		if len(globs) == 0 {
			continue
		}
		res, err := Rotate(site.Path, globs, policy)
		if len(res.Rotated) == 0 && res.Removed == 0 && err == nil {
			continue
		}
		out = append(out, SiteResult{Site: site.Name, Result: res, Err: err})
	}
	return out, nil
}
