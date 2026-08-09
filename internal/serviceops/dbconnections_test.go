package serviceops

import (
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
)

// An install that has configured nothing sees exactly what it saw before: the
// local engines of the family, as containers on the servlo network.
func TestDatabaseConnectionsFor_FallsBackToTheLocalServices(t *testing.T) {
	writeCustomService(t, "postgres-18", "name: postgres-18\nimage: docker.io/postgis/postgis:18-3.6-alpine\nfamily: postgres\n")

	conns := DatabaseConnectionsFor([]string{"postgres"})

	if len(conns) == 0 {
		t.Fatal("no postgres databases found")
	}
	for _, c := range conns {
		if !c.Local || c.Port != 5432 {
			t.Errorf("connection %+v is not a local postgres service", c)
		}
	}
}

// The one the whole story turns on: a managed database has no container, and it
// still has to appear in the admin UI's server list.
func TestDatabaseConnectionsFor_IncludesAManagedDatabase(t *testing.T) {
	writeCustomService(t, "mysql", "name: mysql\nimage: docker.io/library/mysql:8.4\nfamily: mysql\n")
	reg := &dbconn.Registry{}
	if err := reg.Add(dbconn.External("do-managed", "mysql", "db.example.net", 25060, "doadmin", "provider-password")); err != nil {
		t.Fatal(err)
	}
	if err := dbconn.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	conns := DatabaseConnectionsFor([]string{"mysql", "mariadb"})

	var managed *config.DBConnectionInfo
	for i := range conns {
		if conns[i].Name == "do-managed" {
			managed = &conns[i]
		}
	}
	if managed == nil {
		t.Fatalf("the managed database is missing from %+v", conns)
	}
	if managed.Local || managed.Host != "db.example.net" || managed.Port != 25060 || managed.User != "doadmin" {
		t.Errorf("managed connection = %+v", *managed)
	}
	if managed.Password != "provider-password" {
		t.Error("the admin UI cannot log in without the connection's own password")
	}
}

// A local service the registry names is one server, not two: listed once, under
// the name the operator gave it.
func TestDatabaseConnectionsFor_DoesNotListALocalServiceTwice(t *testing.T) {
	writeCustomService(t, "mysql", "name: mysql\nimage: docker.io/library/mysql:8.4\nfamily: mysql\n")
	reg := &dbconn.Registry{}
	if err := reg.Add(dbconn.LocalConnection("primary", "mysql")); err != nil {
		t.Fatal(err)
	}
	if err := dbconn.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	conns := DatabaseConnectionsFor([]string{"mysql"})

	hosts := map[string]int{}
	for _, c := range conns {
		hosts[c.Host]++
	}
	if hosts["servlo-mysql"] != 1 {
		t.Errorf("servlo-mysql listed %d times in %+v", hosts["servlo-mysql"], conns)
	}
	if conns[0].Name != "primary" {
		t.Errorf("name = %q, want the name the operator gave the connection", conns[0].Name)
	}
}

// A family this install has no databases of answers with nothing rather than
// with somebody else's engine.
func TestDatabaseConnectionsFor_UnknownFamilyIsEmpty(t *testing.T) {
	writeCustomService(t, "mysql", "name: mysql\nimage: docker.io/library/mysql:8.4\nfamily: mysql\n")

	if conns := DatabaseConnectionsFor([]string{"redis"}); len(conns) != 0 {
		t.Errorf("got %+v, want nothing for a family that is not a database", conns)
	}
}
