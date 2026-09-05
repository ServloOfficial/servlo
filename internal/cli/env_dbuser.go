package cli

import (
	"strings"

	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbuser"
	"github.com/ServloOfficial/servlo/internal/feedback"
)

// A site's own database account, during `servlo env`.
//
// Which sites get migrated onto one, and when: every site does, on its next
// `servlo env`, and not before. Provisioning is triggered by a definition
// actually asking for {{db_user}} or {{db_password}}, so a site on SQLite or one
// whose framework spells its own credentials out never grows an account it does
// not use, and a site that does use one is moved over exactly when its env file
// is being rewritten anyway. The alternative, migrating every site at once on
// upgrade, would rewrite env files for applications that are running and holding
// the old credentials, which is a way to take a working server down at 3am.
//
// Failure is a warning, not a stop. The site keeps the administrator's
// credentials it is already using, which is a working site with the old
// tradeoff rather than a broken one with the new guarantee.

// wantsDBAccount reports whether any of these declared vars asks for the
// credentials of the site's own database account.
func wantsDBAccount(vars []string) bool {
	for _, v := range vars {
		if strings.Contains(v, "{{db_user}}") || strings.Contains(v, "{{db_password}}") {
			return true
		}
	}
	return false
}

// ensureSiteDBAccount provisions the site's own account and points ctx at it,
// once per run. start brings the local engine up first, because an account
// cannot be created inside a container that is not running.
func ensureSiteDBAccount(ctx *siteTemplateCtx, conn dbconn.Connection, start func(string) error) {
	if ctx == nil || ctx.dbAccountResolved || ctx.site == "" {
		return
	}
	ctx.dbAccountResolved = true

	if conn.Local() && start != nil {
		if err := start(conn.Service); err != nil {
			feedback.Warn("could not start %s, so %s keeps reaching its database as %s: %v",
				conn.Service, ctx.site, conn.User, err)
			return
		}
	}
	cred, err := dbuser.Ensure(conn, ctx.site, []string{ctx.site, ctx.site + "_testing"})
	if err != nil {
		if dbuser.Unsupported(err) {
			// Nothing to say: an engine that issues no accounts is not a
			// failure, it is an engine servlo has no statements for.
			return
		}
		feedback.Warn("could not give %s its own database account, so it keeps reaching the database as %s: %v",
			ctx.site, conn.User, err)
		return
	}
	ctx.dbUser, ctx.dbPassword = cred.User, cred.Password
	envInfo("  %s reaches its database as %q, granted on its own schemas\n", ctx.site, cred.User)
}
