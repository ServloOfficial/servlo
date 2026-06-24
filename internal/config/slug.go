package config

import "strings"

// SiteSlug converts a name (site name, branch, directory basename, or domain)
// to a database-safe, underscore-separated slug. It lowercases and maps every
// character outside [a-z0-9_] to an underscore, extending the long-standing
// hyphen/dot-to-underscore convention to all separators. The mapping matters
// for security: slugs flow into shell-run site_init commands and SQL
// identifiers, so a name carrying a backtick, quote, semicolon, or $(...) must
// not survive into those sinks. Mapping (rather than dropping) keeps slashed
// branches like "feature/x" collision-free as "feature_x".
func SiteSlug(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// WorktreeUnitSlug sanitizes a worktree directory basename for use inside a
// systemd unit name, which treats a dot as the start of the unit-type suffix.
// Only dots are rewritten, so dot-free worktree dirs keep their existing unit
// names (no migration needed) while domain-named dirs (api.gonitro.com-feat)
// stop producing invalid unit names.
func WorktreeUnitSlug(wtBase string) string {
	return strings.ReplaceAll(wtBase, ".", "-")
}
