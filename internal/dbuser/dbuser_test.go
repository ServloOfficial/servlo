package dbuser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/dbcred"
)

// recorder stands in for podman. Every test asserts on what would have been
// run, because the interesting part of provisioning an account is the statement
// and where it is aimed, not that exec returned zero.
type recorder struct {
	runs [][]string
	fail error
}

func (r *recorder) run(args []string, env []string) ([]byte, error) {
	r.runs = append(r.runs, append(append([]string{}, args...), env...))
	return nil, r.fail
}

func (r *recorder) joined() string { return strings.Join(flatten(r.runs), "\n") }

func flatten(runs [][]string) []string {
	out := make([]string, 0, len(runs))
	for _, run := range runs {
		out = append(out, strings.Join(run, " "))
	}
	return out
}

func isolate(t *testing.T) *recorder {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	rec := &recorder{}
	old := runCommand
	runCommand = rec.run
	t.Cleanup(func() { runCommand = old })
	return rec
}

func localMySQL(t *testing.T) dbconn.Connection {
	t.Helper()
	c, err := dbconn.ForService("mysql")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func managedMySQL() dbconn.Connection {
	return dbconn.Connection{
		Name: "do-managed", Family: "mysql",
		Host: "db-mysql-lon1.ondigitalocean.com", Port: 25060,
		User: "doadmin", Password: "adminpasswordadminpassword",
		TLSMode: dbconn.TLSRequire,
	}
}

// The point of the story: the site's account is not the administrator, it is
// granted on its own schema, and the credential comes back for the env file.
func TestEnsure_CreatesAnAccountGrantedOnItsOwnSchema(t *testing.T) {
	rec := isolate(t)
	conn := localMySQL(t)

	cred, err := Ensure(conn, "acme", []string{"acme", "acme_testing"})
	if err != nil {
		t.Fatal(err)
	}
	if cred.User != "acme" {
		t.Errorf("user = %q, want the site's own account", cred.User)
	}
	if cred.User == conn.User {
		t.Error("the site was handed the administrator account")
	}
	if !dbcred.ValidPassword(cred.Password) {
		t.Errorf("password %q is not one servlo generated", cred.Password)
	}

	all := rec.joined()
	if !strings.Contains(all, "CREATE USER IF NOT EXISTS 'acme'@'%'") {
		t.Errorf("no create statement ran:\n%s", all)
	}
	for _, schema := range []string{"acme", "acme_testing"} {
		// The backticks are escaped in the declared statement because it is a
		// double-quoted shell string; what matters is the schema is named.
		want := "GRANT ALL PRIVILEGES ON \\`" + schema + "\\`.*"
		if !strings.Contains(all, want) {
			t.Errorf("no grant on %s:\n%s", schema, all)
		}
	}
	if strings.Contains(all, "ON *.*") || strings.Contains(all, "WITH GRANT OPTION") {
		t.Errorf("the grant is not scoped to the site's schemas:\n%s", all)
	}
}

// The credential is what the site's env file is written from, so it has to
// survive the run that created it.
func TestEnsure_RecordsTheCredential(t *testing.T) {
	isolate(t)
	conn := localMySQL(t)

	cred, err := Ensure(conn, "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := dbcred.For(conn, "acme")
	if !ok {
		t.Fatal("the account was created and not written down, so nothing can write the env file again")
	}
	if stored.User != cred.User || stored.Password != cred.Password {
		t.Errorf("stored %+v, returned %+v", stored, cred)
	}
}

// Re-running env on a site that already has an account must not change its
// password: the running application is holding the old one.
func TestEnsure_IsIdempotent(t *testing.T) {
	isolate(t)
	conn := localMySQL(t)

	first, err := Ensure(conn, "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Ensure(conn, "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Password != second.Password {
		t.Error("a second run rotated the password out from under the running site")
	}
}

// A local database is reached by running the client inside its own container.
func TestEnsure_LocalRunsInsideTheServiceContainer(t *testing.T) {
	rec := isolate(t)

	if _, err := Ensure(localMySQL(t), "acme", nil); err != nil {
		t.Fatal(err)
	}
	first := rec.runs[0]
	if first[0] != "exec" {
		t.Fatalf("local provisioning must exec inside the container, got %v", first)
	}
	if !strings.Contains(strings.Join(first, " "), "servlo-mysql") {
		t.Errorf("exec did not target the service container: %v", first)
	}
}

// A managed database has no container, so the client is run on the servlo
// network and aimed at the provider's host and port.
func TestEnsure_ManagedRunsAClientAgainstTheProvider(t *testing.T) {
	rec := isolate(t)

	if _, err := Ensure(managedMySQL(), "acme", nil); err != nil {
		t.Fatal(err)
	}
	first := strings.Join(rec.runs[0], " ")
	if !strings.HasPrefix(first, "run --rm") {
		t.Fatalf("managed provisioning must run a client, got %q", first)
	}
	for _, want := range []string{
		"--network servlo",
		"-h db-mysql-lon1.ondigitalocean.com",
		"-P 25060",
		"-u doadmin",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("client command missing %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, "servlo-mysql") {
		t.Errorf("a managed database was provisioned against a local container:\n%s", first)
	}
}

// The administrator's password reaches the client through the environment, so
// it is never in an argument list every other process on the box can read.
func TestEnsure_NeverPutsTheAdminPasswordInArgv(t *testing.T) {
	rec := isolate(t)
	conn := managedMySQL()

	if _, err := Ensure(conn, "acme", nil); err != nil {
		t.Fatal(err)
	}
	for _, run := range rec.runs {
		for i, arg := range run {
			if !strings.Contains(arg, conn.Password) {
				continue
			}
			// The one legitimate carrier is the client's own password variable.
			if i > 0 && run[i-1] == "-e" && strings.HasPrefix(arg, "MYSQL_PWD=") {
				continue
			}
			t.Errorf("the admin password appears in argv at %d: %v", i, run)
		}
	}
}

// A provider that requires TLS gets the flags its own client spells, from the
// preset rather than from a switch in Go.
func TestEnsure_ManagedCarriesTheDeclaredTLSFlags(t *testing.T) {
	rec := isolate(t)

	if _, err := Ensure(managedMySQL(), "acme", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.joined(), "--ssl-mode=REQUIRED") {
		t.Errorf("the declared TLS flags did not reach the client:\n%s", rec.joined())
	}
}

// verify-ca needs the certificate inside the container that runs the client,
// and the flags have to name where it landed.
func TestEnsure_ManagedMountsTheCACertificate(t *testing.T) {
	rec := isolate(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caPath, []byte("-----BEGIN CERTIFICATE-----\n"), 0600); err != nil {
		t.Fatal(err)
	}
	conn := managedMySQL()
	conn.TLSMode = dbconn.TLSVerifyCA
	conn.CACert = caPath

	if _, err := Ensure(conn, "acme", nil); err != nil {
		t.Fatal(err)
	}
	first := strings.Join(rec.runs[0], " ")
	if !strings.Contains(first, "-v "+caPath+":"+containerCACert) {
		t.Errorf("the CA certificate was not mounted:\n%s", first)
	}
	if !strings.Contains(first, "--ssl-ca="+containerCACert) {
		t.Errorf("the client was not pointed at the mounted certificate:\n%s", first)
	}
}

// Postgres is a different engine with different statements, and none of that
// difference is in Go.
func TestEnsure_PostgresUsesItsOwnDeclaredStatements(t *testing.T) {
	rec := isolate(t)
	conn, err := dbconn.ForService("postgres")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Ensure(conn, "acme", nil); err != nil {
		t.Fatal(err)
	}
	all := rec.joined()
	if !strings.Contains(all, `CREATE ROLE \"acme\"`) {
		t.Errorf("no role creation ran:\n%s", all)
	}
	if !strings.Contains(all, `ALTER SCHEMA public OWNER TO \"acme\"`) {
		t.Errorf("the site does not own its own schema, so its migrations cannot create tables:\n%s", all)
	}
}

// Rotation changes the password on the server and in the store, and keeps the
// same account: a new user would need its grants again and leave the old one
// behind with rights to the data.
func TestRotate_ChangesThePasswordAndKeepsTheAccount(t *testing.T) {
	rec := isolate(t)
	conn := localMySQL(t)

	before, err := Ensure(conn, "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec.runs = nil

	after, err := Rotate(conn, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if after.User != before.User {
		t.Errorf("user changed from %q to %q", before.User, after.User)
	}
	if after.Password == before.Password {
		t.Error("rotation left the password alone")
	}
	if !strings.Contains(rec.joined(), "ALTER USER 'acme'@'%' IDENTIFIED BY '"+after.Password+"'") {
		t.Errorf("no rotation statement ran:\n%s", rec.joined())
	}
	stored, _ := dbcred.For(conn, "acme")
	if stored.Password != after.Password {
		t.Error("the new password was not written down, so the env file cannot be rewritten from it")
	}
}

// A site that has never had an account gets one rather than an error: rotating
// is also how an operator migrates an old site off the administrator.
func TestRotate_ProvisionsASiteThatHasNoAccountYet(t *testing.T) {
	isolate(t)
	conn := localMySQL(t)

	cred, err := Rotate(conn, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if cred.User != "legacy" || !dbcred.ValidPassword(cred.Password) {
		t.Errorf("rotation of an unprovisioned site returned %+v", cred)
	}
}

// A failure while the server is being changed must not leave the store holding
// a password the server does not have: the site would be locked out at the next
// deploy and nothing would say why.
func TestRotate_KeepsTheOldPasswordWhenTheServerRefuses(t *testing.T) {
	rec := isolate(t)
	conn := localMySQL(t)

	before, err := Ensure(conn, "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec.fail = fmt.Errorf("ERROR 1045 (28000): Access denied")

	if _, err := Rotate(conn, "acme"); err == nil {
		t.Fatal("a refused rotation must be reported")
	}
	stored, _ := dbcred.For(conn, "acme")
	if stored.Password != before.Password {
		t.Error("the store moved on without the server")
	}
}

// An engine whose preset declares no accounts is not a crash: it is a site that
// keeps reaching its database the way it does today.
func TestEnsure_UnsupportedEngineReportsRatherThanGuesses(t *testing.T) {
	isolate(t)
	writeCustomService(t, "quirkdb", "name: quirkdb\nimage: example/quirk:1\nfamily: quirk\n")

	_, err := Ensure(dbconn.Connection{Service: "quirkdb", Family: "quirk"}, "acme", nil)
	if err == nil {
		t.Fatal("an engine with no declared accounts must say so")
	}
	if !Unsupported(err) {
		t.Errorf("err = %v, want one a caller can recognise as unsupported", err)
	}
}

// A connection whose engine servlo does not run locally still resolves its
// statements, from the preset of the dialect it speaks. Without this a managed
// database on an install that has never installed MySQL has no statements at
// all.
func TestSpecFor_ManagedResolvesByDialect(t *testing.T) {
	isolate(t)

	spec, err := specFor(managedMySQL())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Actions["create"]; !ok {
		t.Errorf("resolved spec declares no create: %+v", spec)
	}
	if spec.Image == "" {
		t.Error("a managed database needs a client image to run the statements in")
	}
}

// A managed engine that declares statements but no client image cannot be
// provisioned, and saying so beats running podman with an empty image name.
func TestEnsure_ManagedWithoutAClientImageSaysSo(t *testing.T) {
	isolate(t)
	conn := managedMySQL()
	spec := &config.EntitySpec{Kind: siteUsersKind, Actions: map[string]config.EntityAction{"create": {Exec: "true"}}}

	if _, err := ensureWith(spec, conn, "acme", nil); err == nil {
		t.Fatal("provisioning with no client image must be refused")
	}
}

// Nothing that is not an identifier reaches a statement. The names come from
// site handles and from a file on disk, and both are places a mistake can land.
func TestExpand_RefusesValuesThatCouldBreakTheStatement(t *testing.T) {
	for _, bad := range []string{"acme'; DROP USER root; --", "acme`", "acme user", ""} {
		if _, err := expand("CREATE USER '{{name}}'", map[string]string{"name": bad}); err == nil {
			t.Errorf("name %q must be refused", bad)
		}
	}
	if _, err := expand("x {{host}}", map[string]string{"host": "db.example.com; rm -rf /"}); err == nil {
		t.Error("a host with a shell metacharacter must be refused")
	}
}

// writeCustomService drops a service definition where config will read it.
func writeCustomService(t *testing.T, name, body string) {
	t.Helper()
	dir := filepath.Join(config.ConfigDir(), "services")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
