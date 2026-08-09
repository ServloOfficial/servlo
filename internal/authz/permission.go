package authz

import "strings"

// The permission registry.
//
// Every route the panel serves declares what authority it needs, in one table,
// and the surface scan fails the build on any route that does not appear here.
// That is the point: the enforcement in scope_http.go is only as good as
// somebody remembering to think about a new route, and "remember to think" is
// not a mechanism. The scan turns forgetting into a build failure.
//
// One table rather than a tag beside each handler, because the question an
// operator or a reviewer asks is "what can a developer reach", and that is a
// question you answer by reading a list, not by grepping seventy call sites.

// Permission is the authority a route requires.
type Permission string

const (
	// PermPublic is reachable without a session: the login routes and the
	// things a browser fetches before there is one.
	PermPublic Permission = "public"
	// PermSelf is about the signed-in account itself, so any role may use it.
	PermSelf Permission = "self"
	// PermSite acts on one site, named as the first path segment after the
	// route's prefix. A Developer passes when the site is assigned to them.
	//
	// One site permission rather than a read/write pair: with two roles they
	// would be the same check, and a distinction that changes nothing is a
	// distinction to keep in step for no benefit. Splitting it is the work of
	// whichever story introduces a role that reads without writing.
	PermSite Permission = "site"
	// PermSiteList returns many sites and filters to what the caller may see.
	// Refusing it outright would leave a Developer looking at an error page
	// instead of their own work.
	PermSiteList Permission = "site:list"
	// PermAdmin is everything that is not a site: services, databases,
	// backups, accounts, the panel's own settings, the host itself.
	PermAdmin Permission = "admin"
)

// AllPermissions is every permission a route may declare.
func AllPermissions() []Permission {
	return []Permission{PermPublic, PermSelf, PermSite, PermSiteList, PermAdmin}
}

// Valid reports whether p is a permission servlo knows. Anything else is a
// typo, and a typo that fell through to "allowed" would be an access grant
// spelled as a mistake.
func (p Permission) Valid() bool {
	for _, known := range AllPermissions() {
		if p == known {
			return true
		}
	}
	return false
}

// Registry maps a route pattern to the permission it requires.
type Registry map[string]Permission

// Permissions is the declaration for every route the panel serves.
//
// Patterns are matched by longest prefix, the same way net/http's ServeMux
// resolves them, so a subtree inherits its parent's permission unless it says
// otherwise. That is what lets "/api/sites/" be a read and
// "/api/sites/{domain}/deploy" a write without listing every site action twice.
func Permissions() Registry {
	return Registry{
		// Reachable without a session. The page and its assets are not here
		// because they are not routes the mux answers with data; withPanelAuth
		// serves them and the list lives there.
		"/api/auth/session": PermPublic,
		"/api/auth/login":   PermPublic,
		"/api/auth/logout":  PermPublic,
		"/api/auth/setup":   PermPublic,
		// The deploy webhook holds no session by design: a git host has no
		// cookie. It authenticates with an HMAC signature over the body, keyed
		// by a secret only that one site has.
		"/api/webhooks/deploy/": PermPublic,
		"/api/internal/notify":  PermPublic,

		// Fetched by the browser before there is a session, because they are
		// what renders the login form. They serve files and never state.
		"/manifest.webmanifest":        PermPublic,
		"/sw.js":                       PermPublic,
		"/offline.html":                PermPublic,
		"/icons/icon.svg":              PermPublic,
		"/icons/icon-maskable.svg":     PermPublic,
		"/icons/icon-192.png":          PermPublic,
		"/icons/icon-512.png":          PermPublic,
		"/icons/icon-maskable-192.png": PermPublic,
		"/icons/icon-maskable-512.png": PermPublic,

		// About the signed-in account, so any role may use them.
		"/api/auth/totp/enrol":   PermSelf,
		"/api/auth/totp/confirm": PermSelf,
		"/api/auth/totp/disable": PermSelf,
		"/api/auth/totp/qr":      PermSelf,
		"/api/status":            PermSelf,
		"/api/version":           PermSelf,
		"/api/ws":                PermSelf,
		"/api/access-mode":       PermSelf,

		// Sites. These carry the domain as the first segment after the prefix,
		// which is what makes them checkable against a Developer's list.
		"/api/sites":     PermSiteList,
		"/api/sites/":    PermSite,
		"/api/app-logs/": PermSite,

		// Under the sites tree but not about one site: create and clone take an
		// arbitrary host path, inspect reads one, and reorder rewrites the whole
		// list. Declared above the prefix so they are admin rather than being read
		// as sites named "create" or "clone" that nobody is assigned.
		"/api/sites/create":     PermAdmin,
		"/api/sites/inspect":    PermAdmin,
		"/api/sites/clone":      PermAdmin,
		"/api/sites/clone-test": PermAdmin,
		"/api/sites/deploy-key": PermAdmin,
		"/api/sites/upload":     PermAdmin,
		"/api/sites/reorder":    PermAdmin,

		// Named in the story as things a Developer should reach, and admin
		// anyway for now, because their paths carry a unit or container name
		// rather than a domain. Scoping them needs the routes to say which site
		// they are about; claiming site scope on a path servlo cannot read a
		// site out of would be a leak dressed as a feature. Recorded here so
		// whoever widens them knows what the obstacle was.
		"/api/logs/":          PermAdmin,
		"/api/queue/":         PermAdmin,
		"/api/schedule/":      PermAdmin,
		"/api/horizon/":       PermAdmin,
		"/api/reverb/":        PermAdmin,
		"/api/worker/":        PermAdmin,
		"/api/workers/heal":   PermAdmin,
		"/api/workers/health": PermAdmin,
		"/api/stripe/":        PermAdmin,
		"/api/nginx/":         PermAdmin,
		"/api/certs/alerts":   PermAdmin,

		// Everything else runs the server. A developer deploys applications;
		// they do not restart MySQL, drop a database, add an account, or stop
		// servlo, and the ones that reach the host at all are admin twice over.
		"/api/services":              PermAdmin,
		"/api/services/":             PermAdmin,
		"/api/services/presets":      PermAdmin,
		"/api/services/presets/":     PermAdmin,
		"/api/databases":             PermAdmin,
		"/api/db-connections":        PermAdmin,
		"/api/databases/":            PermAdmin,
		"/api/entities/":             PermAdmin,
		"/api/servlo/start":          PermAdmin,
		"/api/servlo/stop":           PermAdmin,
		"/api/servlo/quit":           PermAdmin,
		"/api/settings":              PermAdmin,
		"/api/settings/autostart":    PermAdmin,
		"/api/settings/worker-mode":  PermAdmin,
		"/api/settings/smtp":         PermAdmin,
		"/api/settings/smtp/test":    PermAdmin,
		"/api/lan/status":            PermAdmin,
		"/api/remote-control":        PermAdmin,
		"/api/php-versions":          PermAdmin,
		"/api/php-versions/":         PermAdmin,
		"/api/php-versions/install":  PermAdmin,
		"/api/php-installable":       PermAdmin,
		"/api/node-versions":         PermAdmin,
		"/api/node-versions/":        PermAdmin,
		"/api/node-versions/install": PermAdmin,
		"/api/node/manage":           PermAdmin,
		"/api/node/unmanage":         PermAdmin,
		"/api/node/set-manager":      PermAdmin,
		"/api/tools/":                PermAdmin,
		"/api/browse":                PermAdmin,
		// SFTP is admin, not site-scoped. Authorising a key hands out
		// filesystem access as the account every site runs as, and until the
		// operator has installed the chroot block it is not confined to one
		// site at all. That is not a developer's call to make.
		"/api/sftp":   PermAdmin,
		"/api/sftp/":  PermAdmin,
		"/api/alerts": PermAdmin,
		// Authorising an SSH key hands out shell access as the account every
		// site runs as. That is never a developer's call, and neither is reading
		// what is listening on a public address.
		"/api/security":              PermAdmin,
		"/api/security/keys":         PermAdmin,
		"/api/audit":                 PermAdmin,
		"/api/stats":                 PermAdmin,
		"/api/disk":                  PermAdmin,
		"/api/watcher/logs":          PermAdmin,
		"/api/watcher/start":         PermAdmin,
		"/api/workspaces":            PermAdmin,
		"/api/workspaces/":           PermAdmin,
		"/api/dashboard-qr":          PermAdmin,
		"/api/queries/route-timing":  PermAdmin,
		"/api/push/devices":          PermAdmin,
		"/api/push/subscribe":        PermSelf,
		"/api/push/unsubscribe":      PermSelf,
		"/api/push/vapid-public-key": PermSelf,
		"/api/push/test":             PermAdmin,
		"/_svc/":                     PermAdmin,
		"/debug/pprof/":              PermAdmin,
	}
}

// For returns the permission a path requires, by longest matching prefix.
//
// Not found means undeclared, and undeclared is never permitted. That is the
// property the surface scan rests on: if this invented an answer for an unknown
// path, the scan could not tell a declared route from one nobody thought about.
func (r Registry) For(path string) (Permission, bool) {
	permission, _, ok := r.lookup(path)
	return permission, ok
}

// lookup is For, and also returns the pattern that matched, which a site-scoped
// route needs in order to know where the domain starts.
func (r Registry) lookup(path string) (Permission, string, bool) {
	if path == "" {
		return "", "", false
	}
	best := ""
	for pattern := range r {
		if !matchesPattern(pattern, path) {
			continue
		}
		if len(pattern) > len(best) {
			best = pattern
		}
	}
	if best == "" {
		return "", "", false
	}
	return r[best], best, true
}

// matchesPattern applies net/http's ServeMux rule: a pattern ending in a slash
// matches its subtree, anything else matches exactly.
func matchesPattern(pattern, path string) bool {
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(path, pattern)
	}
	return path == pattern
}
