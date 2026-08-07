package authz

import "strings"

// What a signed-in account may reach.
//
// Two roles, per PRD §7. An Admin runs the server: every site, plus the things
// that are not a site at all — services, backups, accounts, the panel's own
// settings. A Developer works on the sites assigned to them and sees nothing
// else, which is the shape of a small team where one person owns the box and
// the others own applications on it.
//
// It is deliberately not a permission matrix. Two roles and a list of domains
// is what the PRD asks for and what an operator can hold in their head; S5.5
// adds the per-route declaration that makes the enforcement checkable, not a
// third role.

// Scope is the authority of one request.
type Scope struct {
	Role  Role
	Sites []string
}

// MaySee reports whether this scope may act on a site.
//
// An empty Sites list on a Developer means nothing, not everything. It is the
// state an account is in the moment it is created, and reading it as
// unrestricted would make every new developer an admin until someone noticed.
func (s Scope) MaySee(domain string) bool {
	switch s.Role {
	case RoleAdmin:
		return true
	case RoleDeveloper:
		want := normaliseDomain(domain)
		if want == "" {
			return false
		}
		for _, assigned := range s.Sites {
			if normaliseDomain(assigned) == want {
				return true
			}
		}
		return false
	default:
		// A role servlo does not recognise is a corrupt record. Granting it
		// nothing is the only safe reading; granting it everything is how a
		// typo in a config file becomes an administrator.
		return false
	}
}

// MayAdminister reports whether this scope may touch what is not a site:
// services, backups, accounts, the panel's own settings.
func (s Scope) MayAdminister() bool { return s.Role == RoleAdmin }

// Limited reports whether this scope sees a subset of the sites, so the panel
// can filter rather than showing an empty list and letting the operator wonder.
func (s Scope) Limited() bool { return s.Role != RoleAdmin }

// VisibleSites filters a list of domains down to the ones this scope may see.
func (s Scope) VisibleSites(domains []string) []string {
	if !s.Limited() {
		return domains
	}
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		if s.MaySee(domain) {
			out = append(out, domain)
		}
	}
	return out
}

// normaliseDomain compares domains the way DNS does: case-insensitively and
// without the trailing dot a fully qualified name may carry.
func normaliseDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
}
