package cli

import (
	"os"

	"github.com/ServloOfficial/servlo/internal/config"
	"path/filepath"
	"strings"
	"testing"
)

// isolateConfig keeps command resolution off the developer's real install, so
// the bundled presets are the only declarations in play.
func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// The commands run with the fixed admin credentials in the exec environment,
// never as a -p flag where a process listing would show them.
//
// This test's name has always said that and its assertion did not: it looked
// for MYSQL_PWD= in cmd.Args, which is the argv, so what it actually pinned was
// the password being in the process listing. The name was the invariant, the
// body was the bug, and it read as green either way.
func TestDbImportCmdPasswordOnlyInEnv(t *testing.T) {
	isolateConfig(t)
	env := &dbEnv{service: "mysql", connection: "mysql", database: "testdb"}
	cmd, err := dbImportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "-e MYSQL_PWD") {
		t.Errorf("the credential is not forwarded by name: %v", cmd.Args)
	}
	if !envHasName(cmd.Env, "MYSQL_PWD") {
		t.Error("the credential is not on the process for podman to read")
	}
	assertNoPasswordInArgv(t, cmd.Args)
	for _, arg := range cmd.Args {
		if strings.HasPrefix(arg, "-p") && arg != "-p" {
			t.Errorf("password-like flag in args: %q", arg)
		}
	}
}

func envHasName(env []string, name string) bool {
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && k == name {
			return true
		}
	}
	return false
}

// assertNoPasswordInArgv is the whole point of the two tests that call it.
// /proc/<pid>/cmdline is readable by every process on the machine and every
// site here runs as the same Linux user, so a password in an argument is a
// password every site can read while the command runs.
func assertNoPasswordInArgv(t *testing.T, args []string) {
	t.Helper()
	password, err := config.ServicePassword()
	if err != nil {
		t.Fatalf("reading the install's service password: %v", err)
	}
	if password == "" {
		t.Fatal("no service password, so this check would pass for the wrong reason")
	}
	for i, a := range args {
		if strings.Contains(a, password) {
			t.Errorf("the install's database password is in argv[%d]; every site on this machine can read it out of /proc", i)
		}
	}
}

func TestDbImportCmdMySQLRaisesClientPacket(t *testing.T) {
	isolateConfig(t)
	env := &dbEnv{service: "mysql", connection: "mysql", database: "shop"}
	cmd, err := dbImportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "--max-allowed-packet=") {
		t.Errorf("mysql import must raise the client max_allowed_packet so a large dump is not capped at 16MB: %v", cmd.Args)
	}
}

func TestDbImportCmdPostgresHasNoPacketFlag(t *testing.T) {
	isolateConfig(t)
	env := &dbEnv{service: "postgres", connection: "pgsql", database: "shop"}
	cmd, err := dbImportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(cmd.Args, " "), "max-allowed-packet") {
		t.Errorf("postgres import must not carry a mysql-only flag: %v", cmd.Args)
	}
}

func TestDbCmdPostgresUsesEnv(t *testing.T) {
	isolateConfig(t)
	env := &dbEnv{service: "postgres", connection: "pgsql", database: "testdb"}
	cmd, err := dbImportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "-e PGPASSWORD") {
		t.Errorf("the credential is not forwarded by name: %v", cmd.Args)
	}
	if !envHasName(cmd.Env, "PGPASSWORD") {
		t.Error("the credential is not on the process for podman to read")
	}
	assertNoPasswordInArgv(t, cmd.Args)
}

// The mariadb images carry only the mariadb-named binaries, so the declared
// commands resolve their tool in the container instead of spelling one name.
func TestDbExportCmdMariaDBBinaryFallback(t *testing.T) {
	isolateConfig(t)
	env := &dbEnv{service: "mysql", connection: "mysql", database: "shop"}
	cmd, err := dbExportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "command -v mysqldump || command -v mariadb-dump") {
		t.Errorf("expected mariadb-dump fallback, got: %q", joined)
	}
	if !strings.Contains(joined, "shop") {
		t.Errorf("expected database name in command, got: %q", joined)
	}
}

func TestDbImportCmdMariaDBBinaryFallback(t *testing.T) {
	isolateConfig(t)
	env := &dbEnv{service: "mysql", connection: "mysql", database: "shop"}
	cmd, err := dbImportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "command -v mysql || command -v mariadb") {
		t.Errorf("expected mariadb client fallback, got: %q", joined)
	}
}

// The dump has to be loadable back over a populated database, the same as the
// one a snapshot writes.
func TestDbExportCmdPostgresDropsBeforeCreating(t *testing.T) {
	isolateConfig(t)
	env := &dbEnv{service: "postgres", connection: "pgsql", database: "shop"}
	cmd, err := dbExportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	for _, flag := range []string{"--clean", "--if-exists"} {
		if !strings.Contains(joined, flag) {
			t.Errorf("export missing %s: %q", flag, joined)
		}
	}
}

func TestDbExportCmdMySQLKeepsRoutinesAndEvents(t *testing.T) {
	isolateConfig(t)
	env := &dbEnv{service: "mysql", connection: "mysql", database: "shop"}
	cmd, err := dbExportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	for _, flag := range []string{"--routines", "--triggers", "--events"} {
		if !strings.Contains(joined, flag) {
			t.Errorf("export missing %s: %q", flag, joined)
		}
	}
}

func TestDbCmdUnsupportedConnection(t *testing.T) {
	isolateConfig(t)
	env := &dbEnv{service: "sqlite", connection: "sqlite", database: "shop"}
	_, err := dbImportCmd(env)
	if err == nil {
		t.Error("expected error for unsupported connection")
	}
	_, err = dbExportCmd(env)
	if err == nil {
		t.Error("expected error for unsupported connection")
	}
}

func TestServloServiceFromHost(t *testing.T) {
	cases := map[string]string{
		"servlo-mariadb-11-8": "mariadb-11-8",
		"servlo-mysql":        "mysql",
		"servlo-postgres-18":  "postgres-18",
		"  servlo-mysql ":     "mysql",
		"127.0.0.1":           "",
		"db.example.com":      "",
		"":                    "",
		"servlo-":             "",
	}
	for host, want := range cases {
		if got := servloServiceFromHost(host); got != want {
			t.Errorf("servloServiceFromHost(%q) = %q, want %q", host, got, want)
		}
	}
}

func writeEnvFixture(t *testing.T, lines string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(lines), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadDBEnvTargetsActualServiceFromHost(t *testing.T) {
	// A mariadb-backed site: mysql dialect, but the container is the mariadb one.
	dir := writeEnvFixture(t, "DB_CONNECTION=mysql\nDB_HOST=servlo-mariadb-11-8\nDB_DATABASE=shop\n")
	env, err := loadDBEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if env.service != "mariadb-11-8" {
		t.Errorf("service = %q, want mariadb-11-8", env.service)
	}
	if env.connection != "mysql" {
		t.Errorf("connection = %q, want mysql", env.connection)
	}
}

func TestLoadDBEnvCanonicalHostUnchanged(t *testing.T) {
	dir := writeEnvFixture(t, "DB_CONNECTION=pgsql\nDB_HOST=servlo-postgres\nDB_DATABASE=shop\n")
	env, err := loadDBEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if env.service != "postgres" {
		t.Errorf("service = %q, want postgres", env.service)
	}
}

func TestLoadDBEnvNonServloHostFallsBackToCanonical(t *testing.T) {
	dir := writeEnvFixture(t, "DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_DATABASE=shop\n")
	env, err := loadDBEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if env.service != "mysql" {
		t.Errorf("service = %q, want mysql (canonical fallback)", env.service)
	}
}
