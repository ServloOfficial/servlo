package dbuser

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
)

// A site's account must not be able to reach another site's database.
//
// There are two routes to the same answer and they disagreed. A database servlo
// does not host is provisioned over the network by dbconn, which creates the
// database, revokes CONNECT from PUBLIC and then grants it to the one role.
// The revoke is not decoration: PostgreSQL grants CONNECT on a new database to
// PUBLIC, every role in the cluster is in PUBLIC, and on a shared server that
// cluster holds every other site. dbconn's own comment says exactly that.
//
// A database servlo does host is provisioned by the statements the preset
// declares, and those never revoked anything. So the reasoning was written
// down, applied to the managed server, and not applied to the container that
// most sites actually run on.
func TestPostgresSiteUsersRevokeConnectFromPublic(t *testing.T) {
	spec := postgresSiteUsersSpec(t)

	grant, ok := spec.Actions["grant"]
	if !ok || grant.Exec == "" {
		t.Fatal("the postgres preset declares no grant action for site users")
	}
	body := grant.Exec

	if !strings.Contains(strings.ToUpper(body), "REVOKE CONNECT ON DATABASE") {
		t.Errorf("the postgres site-user grant never revokes PUBLIC's CONNECT, so every site's "+
			"role can reach every other site's database on the same server:\n%s", body)
	}
	if !strings.Contains(strings.ToUpper(body), "FROM PUBLIC") {
		t.Errorf("the postgres site-user grant revokes from somebody other than PUBLIC:\n%s", body)
	}
	// The site itself still has to get in.
	if !strings.Contains(strings.ToUpper(body), "GRANT CONNECT ON DATABASE") {
		t.Errorf("the postgres site-user grant does not give the site's own role CONNECT:\n%s", body)
	}
}

// The MySQL family has no PUBLIC to revoke from, and its protection is that the
// grant names one database. A grant on *.* would hand every site the whole
// server, which is the same failure by the other engine's spelling.
func TestMySQLSiteUsersGrantOneDatabaseOnly(t *testing.T) {
	for _, name := range []string{"mysql", "mariadb"} {
		p, err := config.LoadPreset(name)
		if err != nil {
			t.Logf("%s: not present in this build (%v)", name, err)
			continue
		}
		if p.Introspect == nil {
			continue
		}
		spec := p.Introspect.Entity(siteUsersKind)
		if spec == nil {
			continue
		}
		body := spec.Actions["grant"].Exec
		if body == "" {
			t.Errorf("%s declares no grant action for site users", name)
			continue
		}
		if strings.Contains(body, "ON *.*") || strings.Contains(body, "ON `*`.*") {
			t.Errorf("%s grants a site's account the whole server:\n%s", name, body)
		}
		if !strings.Contains(body, "{{database}}") {
			t.Errorf("%s's grant does not name the site's own database, so what it scopes to "+
				"cannot be read from the statement:\n%s", name, body)
		}
	}
}

func postgresSiteUsersSpec(t *testing.T) *config.EntitySpec {
	t.Helper()
	spec := presetSpecForDialect(dbconn.Dialect("postgres"))
	if spec == nil {
		t.Fatal("no preset declares site_users for postgres, so this check proved nothing")
	}
	return spec
}
