package config

// A staging site is a site. It has its own directory, its own domain, its own
// database and its own certificate, and every part of servlo treats it like any
// other. What this block adds is the three things that make it staging rather
// than a second production site:
//
//   - it remembers which live site it is a copy of, so a refresh knows what to
//     copy and, more importantly, which direction is forbidden
//   - it is not indexed
//   - it is behind a password
//
// The last two are not optional and there is no switch for them. A staging site
// that search engines index is duplicate content against the real site, and a
// staging site anyone can open is a half-finished feature and a copy of the
// client's data on a public address. Both of those are discovered late and by
// somebody else.

// SiteStaging marks a site as a staging copy of another.
type SiteStaging struct {
	// Origin is the live site this one copies from. A staging site with no
	// origin is still a staging site: it is not indexed and it is behind a
	// password, and only the refresh has nothing to do.
	Origin string `yaml:"origin,omitempty" json:"origin,omitempty"`
	// User and Hash are the HTTP basic credentials nginx checks.
	//
	// The hash and never the password. Nobody can be shown this password again
	// after it is set, which is deliberate: it is generated, it is long, and it
	// is meant to be pasted into a password manager once rather than remembered.
	User string `yaml:"user,omitempty" json:"user,omitempty"`
	Hash string `yaml:"hash,omitempty" json:"-"`
	// RefreshedAt is when live was last copied over this site, RFC3339. Shown
	// in the panel because the question an operator asks about a staging site
	// is almost always how old it is.
	RefreshedAt string `yaml:"refreshed_at,omitempty" json:"refreshed_at,omitempty"`
}

// IsStaging reports whether this site is a staging copy.
func (s *Site) IsStaging() bool { return s != nil && s.Staging != nil }

// StagingOrigin is the live site this one copies from, empty when it is not a
// staging site or has no origin.
func (s *Site) StagingOrigin() string {
	if !s.IsStaging() {
		return ""
	}
	return s.Staging.Origin
}
