package dbconn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// freePort returns a port on loopback with nothing listening on it, so a dial
// there is refused immediately rather than hanging.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// A database servlo runs is reached through its container, on a network the
// panel process is not on. Dialling it here would fail with "no such host" and
// send the operator to their firewall over a service that is running fine.
func TestTest_SaysALocalServiceIsNotReachedThisWay(t *testing.T) {
	isolate(t)

	err := Test(LocalConnection("local", "mysql"))
	if err == nil {
		t.Fatal("a local service was tested over the network")
	}
	if !strings.Contains(err.Error(), "mysql") {
		t.Errorf("error = %q, does not name the service", err)
	}
}

// The failure that matters most: the host is up, the credentials are right, and
// the provider is dropping the packet because this server is not in its trusted
// sources. Saying "connection refused" alone is what makes that a support
// ticket instead of a two-minute fix.
func TestTest_CannotReachTheHostNamesTrustedSources(t *testing.T) {
	isolate(t)

	for _, engine := range []string{"mysql", "postgres"} {
		c := External("managed", engine, "127.0.0.1", freePort(t), "admin", "pw")
		err := Test(c)
		if err == nil {
			t.Fatalf("%s: a dial to a closed port succeeded", engine)
		}
		msg := err.Error()
		if !strings.Contains(msg, "127.0.0.1") {
			t.Errorf("%s: error = %q, does not say where it tried", engine, msg)
		}
		if !strings.Contains(msg, "trusted sources") {
			t.Errorf("%s: error = %q, does not mention the provider's trusted sources", engine, msg)
		}
	}
}

// A host that accepts the connection and then says nothing must not hold the
// panel open. An operator waiting on a spinner cannot tell a slow database from
// a hung one.
func TestTest_TimesOutRatherThanHanging(t *testing.T) {
	isolate(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Accept and say nothing, which is what a black-holing middlebox
			// looks like from here.
			defer conn.Close()
		}
	}()

	prev := netTimeout
	netTimeout = 300 * time.Millisecond
	defer func() { netTimeout = prev }()

	port := ln.Addr().(*net.TCPAddr).Port
	for _, engine := range []string{"mysql", "postgres"} {
		done := make(chan error, 1)
		go func() { done <- Test(External("managed", engine, "127.0.0.1", port, "admin", "pw")) }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: a silent host reported a working connection", engine)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: Test did not return; it has no timeout", engine)
		}
	}
}

// The four answers an operator can act on, told apart. "Failed to connect" for
// all of them is the same as no message at all.
func TestConnectionError_TellsTheCausesApart(t *testing.T) {
	c := External("managed", "mysql", "db.example.net", 25060, "doadmin", "s3cret")

	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "the host is not reachable",
			err:  &net.OpError{Op: "dial", Err: errors.New("connection refused")},
			want: "trusted sources",
		},
		{
			name: "the host does not resolve",
			err:  &net.DNSError{Err: "no such host", Name: "db.example.net", IsNotFound: true},
			want: "does not resolve",
		},
		{
			name: "the credentials were refused by mysql",
			err:  &mysql.MySQLError{Number: 1045, Message: "Access denied for user 'doadmin'@'1.2.3.4'"},
			want: "refused the credentials",
		},
		{
			name: "the credentials were refused by postgres",
			err:  &pgconn.PgError{Code: "28P01", Message: "password authentication failed"},
			want: "refused the credentials",
		},
		{
			name: "the certificate does not verify",
			err:  &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
			want: "TLS",
		},
		{
			name: "reached, and something else went wrong",
			err:  &pgconn.PgError{Code: "42501", Message: "permission denied to create database"},
			want: "permission denied to create database",
		},
	}
	for _, tc := range cases {
		got := connectionError(c, tc.err)
		if got == nil {
			t.Errorf("%s: no error", tc.name)
			continue
		}
		if !strings.Contains(got.Error(), tc.want) {
			t.Errorf("%s: error = %q, want it to mention %q", tc.name, got, tc.want)
		}
	}
}

// A driver that echoes the DSN into its error would put a managed database's
// password in the panel, the audit log and every screenshot of either.
func TestConnectionError_NeverCarriesThePassword(t *testing.T) {
	c := External("managed", "postgres", "db.example.net", 25060, "doadmin", "hunter2")

	err := connectionError(c, errors.New("failed to connect: host=db.example.net user=doadmin password=hunter2"))
	if err == nil {
		t.Fatal("no error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the error carries the password: %s", err)
	}
	if !strings.Contains(err.Error(), "****") {
		t.Errorf("error = %q, want the credential redacted rather than dropped", err)
	}
}

// A least-privilege user gets its own schema and nothing else. The grant that
// matters is the one that is not there: ON *.* would hand a site every other
// site's data on the same server.
func TestMySQLProvision_GrantsOnlyTheSitesOwnSchema(t *testing.T) {
	stmts := mysqlProvisionStatements("shop", "shop")

	joined := strings.Join(stmts, "\n")
	if strings.Contains(joined, "*.*") {
		t.Errorf("the grant is server-wide:\n%s", joined)
	}
	if !strings.Contains(joined, "GRANT ALL PRIVILEGES ON `shop`.*") {
		t.Errorf("no grant on the site's own schema:\n%s", joined)
	}
	if !strings.Contains(joined, "CREATE DATABASE IF NOT EXISTS `shop`") {
		t.Errorf("the database is not created idempotently:\n%s", joined)
	}
	if strings.Contains(joined, "GRANT ALL PRIVILEGES ON `shop`.* TO 'shop'@'%' WITH GRANT OPTION") {
		t.Errorf("the site user can hand its rights on:\n%s", joined)
	}
}

// PostgreSQL's default is the opposite way round: every role can connect to
// every database in the cluster unless PUBLIC is revoked, which on a managed
// server means every other site.
func TestPostgresProvision_ShutsThePublicRoleOut(t *testing.T) {
	stmts := postgresProvisionStatements("shop", "shop")

	joined := strings.Join(stmts, "\n")
	if !strings.Contains(joined, `REVOKE CONNECT ON DATABASE "shop" FROM PUBLIC`) {
		t.Errorf("PUBLIC keeps its connect right:\n%s", joined)
	}
	if !strings.Contains(joined, `GRANT ALL PRIVILEGES ON DATABASE "shop" TO "shop"`) {
		t.Errorf("the site user has no rights on its own database:\n%s", joined)
	}
	if strings.Contains(joined, "SUPERUSER") || strings.Contains(joined, "CREATEDB") {
		t.Errorf("the role is created with more than it needs:\n%s", joined)
	}
}

// A name is pasted into SQL that no placeholder can carry, so anything that is
// not a plain identifier is refused before it gets there.
func TestCreateDatabaseAndUser_RefusesAnIdentifierThatIsNotOne(t *testing.T) {
	isolate(t)

	c := External("managed", "mysql", "127.0.0.1", freePort(t), "admin", "pw")
	bad := []string{
		"shop`; DROP DATABASE mysql; --",
		`shop"`,
		"shop'",
		"shop db",
		"",
		strings.Repeat("s", 100),
	}
	// The port is closed, so every call fails one way or another: what is being
	// checked is that it failed on the name rather than reaching the network
	// with it.
	for _, name := range bad {
		_, err := CreateDatabaseAndUser(c, name, "site")
		if err == nil || !strings.Contains(err.Error(), "not a usable database name") {
			t.Errorf("database name %q: error = %v, want it refused as a name", name, err)
		}
		_, err = CreateDatabaseAndUser(c, "shop", name)
		if err == nil || !strings.Contains(err.Error(), "not a usable database user name") {
			t.Errorf("user name %q: error = %v, want it refused as a name", name, err)
		}
	}
}

// Creating a database on a service servlo runs goes through the container, not
// over the network, and saying so is better than a dial that cannot work.
func TestCreateDatabaseAndUser_SaysALocalServiceIsProvisionedElsewhere(t *testing.T) {
	isolate(t)

	if _, err := CreateDatabaseAndUser(LocalConnection("local", "mysql"), "shop", "shop"); err == nil {
		t.Fatal("a local service was provisioned over the network")
	}
}

// The generated password has to survive being pasted into a SQL statement and
// into an env file, so it carries nothing that quotes or escapes.
func TestGeneratedPassword_IsStrongAndSurvivesBothPlaces(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		pw := generatePassword()
		if len(pw) < 24 {
			t.Fatalf("password %q is %d characters, too short to be worth generating", pw, len(pw))
		}
		if strings.ContainsAny(pw, "'\"`\\ \n$#") {
			t.Fatalf("password %q carries a character that quotes, escapes or comments", pw)
		}
		if seen[pw] {
			t.Fatal("the same password was generated twice")
		}
		seen[pw] = true
	}
}

// Test honours a caller's deadline, so a panel request that is abandoned does
// not leave a dial running behind it.
func TestTestContext_StopsWhenTheCallerDoes(t *testing.T) {
	isolate(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := TestContext(ctx, External("managed", "mysql", "127.0.0.1", freePort(t), "a", "b")); err == nil {
		t.Fatal("a cancelled test reported a working connection")
	}
}
