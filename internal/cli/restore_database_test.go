package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/dbconn"
)

// A rebuild restores onto a machine that has the registry and the credentials
// but none of the databases: a state archive carries what servlo knows, not the
// engine's contents. A dump loads into a database rather than creating one, so
// without this the rebuild stops at the first site with "Unknown database".
func TestEnsureRestoreDatabase_CreatesItOnALocalService(t *testing.T) {
	var gotSvc, gotName string
	defer swapCreateRestoreDatabase(func(svc, name string) (bool, error) {
		gotSvc, gotName = svc, name
		return true, nil
	})()

	if err := ensureRestoreDatabase(dbconn.Connection{Service: "mysql"}, "shop"); err != nil {
		t.Fatalf("ensureRestoreDatabase: %v", err)
	}
	if gotSvc != "mysql" || gotName != "shop" {
		t.Errorf("created %q/%q, want mysql/shop", gotSvc, gotName)
	}
}

// A managed database belongs to the provider, and servlo has no way to create
// one there. PRD 3.6: never assume the database is local.
func TestEnsureRestoreDatabase_LeavesAManagedConnectionAlone(t *testing.T) {
	called := false
	defer swapCreateRestoreDatabase(func(string, string) (bool, error) {
		called = true
		return false, nil
	})()

	conn := dbconn.Connection{Host: "db.example.com", Port: 3306}
	if err := ensureRestoreDatabase(conn, "shop"); err != nil {
		t.Fatalf("ensureRestoreDatabase: %v", err)
	}
	if called {
		t.Error("servlo tried to create a database on a connection it does not run")
	}
}

// The files are already back by this point, so the failure has to say which
// half did not land and which database it was.
func TestEnsureRestoreDatabase_SaysWhichDatabaseItCouldNotCreate(t *testing.T) {
	defer swapCreateRestoreDatabase(func(string, string) (bool, error) {
		return false, errors.New("engine refused")
	})()

	err := ensureRestoreDatabase(dbconn.Connection{Service: "mysql"}, "shop")
	if err == nil {
		t.Fatal("a failed create was reported as success")
	}
	if !strings.Contains(err.Error(), "shop") {
		t.Errorf("error %q does not name the database", err)
	}
}

// swapCreateRestoreDatabase installs a stub and returns the restore func.
func swapCreateRestoreDatabase(stub func(string, string) (bool, error)) func() {
	prev := createRestoreDatabase
	createRestoreDatabase = stub
	return func() { createRestoreDatabase = prev }
}
