package cli

import (
	"strings"
	"testing"
)

func TestDbImportCmdMySQLPasswordNotInArgs(t *testing.T) {
	env := &dbEnv{
		connection: "mysql",
		database:   "testdb",
		username:   "root",
		password:   "secret123",
	}
	cmd, err := dbImportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range cmd.Args {
		if strings.Contains(arg, "secret123") && !strings.HasPrefix(arg, "MYSQL_PWD=") {
			t.Errorf("password leaked in command arg: %q", arg)
		}
	}
	// Verify password is passed via env, not -p flag
	for _, arg := range cmd.Args {
		if strings.HasPrefix(arg, "-psecret123") || strings.HasPrefix(arg, "-p=secret123") {
			t.Errorf("password passed via -p flag: %q", arg)
		}
	}
}

func TestDbExportCmdMySQLPasswordNotInArgs(t *testing.T) {
	env := &dbEnv{
		connection: "mysql",
		database:   "testdb",
		username:   "root",
		password:   "secret123",
	}
	cmd, err := dbExportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range cmd.Args {
		if strings.Contains(arg, "secret123") && !strings.HasPrefix(arg, "MYSQL_PWD=") {
			t.Errorf("password leaked in command arg: %q", arg)
		}
	}
}

func TestDbCmdPostgresUsesEnv(t *testing.T) {
	env := &dbEnv{
		connection: "pgsql",
		database:   "testdb",
		username:   "postgres",
		password:   "secret123",
	}
	cmd, err := dbImportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, arg := range cmd.Args {
		if arg == "PGPASSWORD=secret123" {
			found = true
		}
	}
	if !found {
		t.Error("expected PGPASSWORD env var in postgres command")
	}
}

func TestDbExportCmdMariaDBBinaryFallback(t *testing.T) {
	env := &dbEnv{connection: "mariadb", database: "shop", username: "root", password: "lerd"}
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
	env := &dbEnv{connection: "mysql", database: "shop", username: "root", password: "lerd"}
	cmd, err := dbImportCmd(env)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "command -v mysql || command -v mariadb") {
		t.Errorf("expected mariadb client fallback, got: %q", joined)
	}
}

func TestDbCmdUnsupportedConnection(t *testing.T) {
	env := &dbEnv{connection: "sqlite"}
	_, err := dbImportCmd(env)
	if err == nil {
		t.Error("expected error for unsupported connection")
	}
	_, err = dbExportCmd(env)
	if err == nil {
		t.Error("expected error for unsupported connection")
	}
}
