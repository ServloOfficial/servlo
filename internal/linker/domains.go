package linker

import (
	"fmt"

	"github.com/ServloOfficial/servlo/internal/config"
)

// reservedDomains are domains servlo itself uses, which cannot be assigned to a
// user site.
var reservedDomains = []string{}

// IsReservedDomain reports whether a domain is reserved for servlo's own use.
func IsReservedDomain(domain string) bool {
	for _, r := range reservedDomains {
		if domain == r {
			return true
		}
	}
	return false
}

// FreeSiteName returns the first available site name for a path. An unused
// name is returned as-is, as is one already held by the same path (a re-link).
// A name held by a different path gets "-2", "-3", … until one is free.
func FreeSiteName(desired, path string) string {
	for i := 0; ; i++ {
		candidate := desired
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", desired, i+1)
		}
		existing, err := config.FindSite(candidate)
		if err != nil || existing == nil {
			return candidate // name is free
		}
		if config.CanonicalPath(existing.Path) == config.CanonicalPath(path) {
			return candidate // same site being re-registered (symlink spellings included, #930)
		}
	}
}

// FilterConflictingDomains splits desired into the domains ownPath may claim
// and those a different site already holds. The check is strict: a domain is a
// conflict regardless of TLS scheme, because DNS and browser caches don't
// disambiguate by scheme reliably. Order is preserved so a surviving preferred
// domain stays primary. Re-linking the same path is not a conflict.
func FilterConflictingDomains(desired []string, ownPath string, allSites []config.Site) (kept, removed []string) {
	owners := make(map[string]string, len(allSites)*2)
	for _, s := range allSites {
		for _, d := range s.Domains {
			owners[d] = s.Path
		}
	}

	for _, d := range desired {
		if IsReservedDomain(d) {
			removed = append(removed, d)
			continue
		}
		owner, taken := owners[d]
		if taken && owner != ownPath {
			removed = append(removed, d)
			continue
		}
		kept = append(kept, d)
	}
	return kept, removed
}

// ResolveDomains filters the desired domain list against the live registry and
// returns the list to register. There is no fallback: a conflicted domain used
// to be replaced by a freshly generated `<name>.<tld>`, which only worked while
// servlo owned a TLD it could invent inside. A caller that gets nothing back
// must report the conflict, because the operator's domain is the only one that
// resolves here. The .servlo.yaml on disk is never touched; the dropped domains
// come back in removed so the caller can name them.
func ResolveDomains(desired []string, ownPath string) (kept, removed []string) {
	reg, err := config.LoadSites()
	var sites []config.Site
	if err == nil {
		sites = reg.Sites
	}
	return FilterConflictingDomains(desired, ownPath, sites)
}
