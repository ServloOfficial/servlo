package dbdump

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/dbexec"
)

// record swaps the runner for one that remembers the argv it was handed, which
// is what every assertion here is about: a dump is the statement that ran and
// the server it was aimed at.
func record(t *testing.T, out string, fail error) *[][]string {
	t.Helper()
	var runs [][]string
	prev := dbexec.Run
	t.Cleanup(func() { dbexec.Run = prev })
	dbexec.Run = func(_ context.Context, args []string, _ []string, stdout io.Writer) ([]byte, error) {
		runs = append(runs, args)
		if stdout != nil && out != "" {
			_, _ = io.WriteString(stdout, out)
		}
		if fail != nil {
			return []byte("mysqldump: Got error 2002"), fail
		}
		return nil, nil
	}
	return &runs
}

func mysqlSpec() *config.EntitySpec {
	return &config.EntitySpec{
		Kind:  "databases",
		Image: "docker.io/library/mysql:8.4",
		TLS:   map[string]string{"verify-ca": "--ssl-mode=VERIFY_CA --ssl-ca={{ca_cert}}"},
		Actions: map[string]config.EntityAction{
			"remote_export": {Exec: "mysqldump -h {{host}} -P {{port}} -u {{admin_user}} {{tls_flags}} --single-transaction {{database}}"},
			"export":        {Exec: "mysqldump -uroot --single-transaction {{name}}"},
		},
	}
}

// A database servlo runs is dumped inside its own container, which is where the
// engine and its client already are.
func TestDump_LocalRunsInsideTheServiceContainer(t *testing.T) {
	runs := record(t, "-- dump", nil)
	conn := dbconn.Connection{Name: "local", Family: "mysql", Service: "mysql", User: "root", Password: "servlopw"}

	var out strings.Builder
	if err := dumpWith(mysqlSpec(), conn, "acme", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "-- dump" {
		t.Errorf("the dump did not reach the writer, got %q", out.String())
	}
	argv := strings.Join((*runs)[0], " ")
	if !strings.Contains(argv, "exec") || !strings.Contains(argv, "servlo-mysql") {
		t.Errorf("a local dump did not run in the service container:\n%s", argv)
	}
}

// The case this package exists for: a managed database has no container here,
// so the client runs on the servlo network aimed at the provider. Without it a
// managed site's backup carries its files and none of its data.
func TestDump_ManagedRunsAClientAimedAtTheProvider(t *testing.T) {
	runs := record(t, "-- dump", nil)
	conn := dbconn.Connection{
		Name: "managed", Family: "mysql", Host: "db.example.net", Port: 25060,
		User: "doadmin", Password: "adminpw", TLSMode: dbconn.TLSVerifyCA, CACert: "/etc/servlo/db-ca/managed.crt",
	}

	var out strings.Builder
	if err := dumpWith(mysqlSpec(), conn, "acme", &out); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join((*runs)[0], " ")
	for _, want := range []string{"run --rm", "--network servlo", "db.example.net", "25060", "doadmin", "--ssl-mode=VERIFY_CA"} {
		if !strings.Contains(argv, want) {
			t.Errorf("a managed dump is missing %q:\n%s", want, argv)
		}
	}
	if strings.Contains(argv, "adminpw") {
		t.Error("the administrator's password is in the argument list, where the rest of the machine can read it")
	}
	if !strings.Contains(argv, "-v /etc/servlo/db-ca/managed.crt:") {
		t.Errorf("the CA certificate was not mounted:\n%s", argv)
	}
}

// A dump that fails must say so. Returning a short or empty file as a backup is
// the failure the whole test-restore story exists to catch, and catching it
// here is cheaper.
func TestDump_ReportsAFailureRatherThanAnEmptyFile(t *testing.T) {
	record(t, "", errors.New("exit status 2"))
	conn := dbconn.Connection{Name: "managed", Family: "mysql", Host: "db.example.net", Port: 25060, User: "doadmin", Password: "adminpw12"}

	var out strings.Builder
	err := dumpWith(mysqlSpec(), conn, "acme", &out)
	if err == nil {
		t.Fatal("a failed dump reported success")
	}
	if !strings.Contains(err.Error(), "2002") {
		t.Errorf("error = %q, does not carry what the engine said", err)
	}
	if strings.Contains(err.Error(), "adminpw12") {
		t.Error("the administrator's password is in the error text")
	}
}

// An engine whose definition declares no remote dump says so plainly. Falling
// back to the local statement would aim a dump at 127.0.0.1 inside a throwaway
// container and produce an empty file with no error.
func TestDump_RefusesAManagedConnectionWithNoDeclaredRemoteDump(t *testing.T) {
	record(t, "", nil)
	spec := mysqlSpec()
	delete(spec.Actions, "remote_export")
	conn := dbconn.Connection{Name: "managed", Family: "mysql", Host: "db.example.net", Port: 25060, User: "doadmin"}

	var out strings.Builder
	if err := dumpWith(spec, conn, "acme", &out); err == nil {
		t.Fatal("a managed dump with no declared statement reported success")
	}
}
