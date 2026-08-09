package dbconn

import (
	"os"
	"strings"
	"testing"
)

// An install that has never configured a connection keeps working exactly as it
// did: its sites name a service, and a service is a connection. Requiring a
// migration to keep a running server running is not a feature.
func TestNamed_FallsBackToTheLocalService(t *testing.T) {
	password := isolate(t)

	c, err := Named("mysql")
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "servlo-mysql" || c.Password != password {
		t.Errorf("connection = %+v, want the local mysql service", c)
	}
}

// The default is what a new site gets. On an install with no registry it is the
// local MySQL, which is where every site has gone since before this existed.
func TestDefault_IsTheLocalMySQLUntilSomethingSaysOtherwise(t *testing.T) {
	isolate(t)

	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if c.Service != "mysql" || !c.Local() {
		t.Errorf("default = %+v, want the local mysql service", c)
	}
}

// An external database is a first-class connection, and reading it back gives
// the same thing that was written, credentials included.
func TestRegistry_RoundTripsAnExternalConnection(t *testing.T) {
	isolate(t)

	want := External("managed", "postgres", "db.example.net", 25060, "doadmin", "s3cret")
	want.TLSMode = TLSRequire

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(want); err != nil {
		t.Fatal(err)
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	got, err := Named("managed")
	if err != nil {
		t.Fatal(err)
	}
	if got.Local() {
		t.Error("a managed database read back as a local service")
	}
	if got.Host != "db.example.net" || got.Port != 25060 || got.User != "doadmin" || got.Password != "s3cret" {
		t.Errorf("connection = %+v, want the one that was stored", got)
	}
	if got.TLSMode != TLSRequire {
		t.Errorf("TLS mode = %q, want %q", got.TLSMode, TLSRequire)
	}
}

// The file holds an external database's password, so it is readable by nobody
// else. Every site on this machine runs as the same user, and a mode of 0644
// would put a managed database's credentials inside reach of all of them.
func TestSaveRegistry_IsUnreadableByAnybodyElse(t *testing.T) {
	isolate(t)

	reg := &Registry{}
	if err := reg.Add(External("managed", "mysql", "db.example.net", 3306, "admin", "s3cret")); err != nil {
		t.Fatal(err)
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(registryFile())
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("mode = %04o, want 0600", mode)
	}
}

// A local connection stores which service it is and nothing else. Writing its
// password down would leave a second copy behind to go stale the moment the
// operator rotates the first.
func TestSaveRegistry_DoesNotWriteALocalPasswordDown(t *testing.T) {
	password := isolate(t)

	reg := &Registry{}
	if err := reg.Add(LocalConnection("local", "mysql")); err != nil {
		t.Fatal(err)
	}
	if err := SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(registryFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), password) {
		t.Errorf("the service password was written into the registry:\n%s", data)
	}

	// And it still resolves with one, because it is derived rather than stored.
	c, err := Named("local")
	if err != nil {
		t.Fatal(err)
	}
	if c.Password != password {
		t.Errorf("password = %q, want the install's own", c.Password)
	}
}

// The first connection added becomes the default, because an install with one
// connection and no default is a state nobody meant to be in.
func TestAdd_TheFirstConnectionBecomesTheDefault(t *testing.T) {
	isolate(t)

	reg := &Registry{}
	if err := reg.Add(External("first", "mysql", "a.example.net", 3306, "admin", "pw")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(External("second", "postgres", "b.example.net", 5432, "admin", "pw")); err != nil {
		t.Fatal(err)
	}
	if reg.Default != "first" {
		t.Errorf("default = %q, want the first one added", reg.Default)
	}
}

// Removing the default hands the role to something else rather than leaving the
// install unable to create a site.
func TestRemove_MovesTheDefaultOn(t *testing.T) {
	isolate(t)

	reg := &Registry{}
	_ = reg.Add(External("first", "mysql", "a.example.net", 3306, "admin", "pw"))
	_ = reg.Add(External("second", "postgres", "b.example.net", 5432, "admin", "pw"))

	if err := reg.Remove("first"); err != nil {
		t.Fatal(err)
	}
	if reg.Default != "second" {
		t.Errorf("default = %q, want the remaining connection", reg.Default)
	}
	if _, ok := reg.Find("first"); ok {
		t.Error("the removed connection is still there")
	}
}

// A default naming something that is gone is an error rather than a quiet
// substitution. Putting a site's data somewhere the operator did not choose is
// worse than refusing to create it.
func TestDefault_RefusesToGuessWhenItNamesNothing(t *testing.T) {
	isolate(t)

	if err := SaveRegistry(&Registry{Default: "gone"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Default(); err == nil {
		t.Fatal("a default naming a connection that does not exist was accepted")
	}
}

// A connection is checked when it is added, not when a deploy first uses it. By
// then the site exists and its .env is written against a database nothing can
// reach.
func TestValidate_RefusesAHalfConfiguredConnection(t *testing.T) {
	cases := map[string]Connection{
		"no name":                        External("", "mysql", "a.example.net", 3306, "admin", "pw"),
		"a name with a slash":            External("a/b", "mysql", "a.example.net", 3306, "admin", "pw"),
		"an engine servlo cannot manage": External("c", "cassandra", "a.example.net", 9042, "admin", "pw"),
		"no host":                        External("c", "mysql", "", 3306, "admin", "pw"),
		"no user":                        External("c", "mysql", "a.example.net", 3306, "", "pw"),
		"no password":                    External("c", "mysql", "a.example.net", 3306, "admin", ""),
		"an impossible port":             {Name: "c", Family: "mysql", Host: "a.example.net", Port: 70000, User: "admin", Password: "pw"},
		"an unknown TLS mode":            {Name: "c", Family: "mysql", Host: "a.example.net", Port: 3306, User: "admin", Password: "pw", TLSMode: "maybe"},
		"verify-ca with nothing to verify against": {Name: "c", Family: "mysql", Host: "a.example.net", Port: 3306, User: "admin", Password: "pw", TLSMode: TLSVerifyCA},
	}
	for name, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	// And a complete one is not refused, or the check is just an obstacle.
	ok := External("managed", "postgres", "db.example.net", 25060, "doadmin", "pw")
	ok.TLSMode = TLSRequire
	if err := ok.Validate(); err != nil {
		t.Errorf("a complete connection was refused: %v", err)
	}
	if err := LocalConnection("local", "mysql").Validate(); err != nil {
		t.Errorf("a local connection was refused: %v", err)
	}
}

// Two connections with one name is an install where a site's database depends
// on iteration order.
func TestAdd_RefusesADuplicateName(t *testing.T) {
	isolate(t)

	reg := &Registry{}
	_ = reg.Add(External("managed", "mysql", "a.example.net", 3306, "admin", "pw"))
	if err := reg.Add(External("managed", "postgres", "b.example.net", 5432, "admin", "pw")); err == nil {
		t.Fatal("a second connection took a name that was already in use")
	}
}

// A managed provider that did not move the port should not have to be told what
// its engine's port is.
func TestExternal_DefaultsThePortToTheEngine(t *testing.T) {
	if got := External("c", "postgres", "db.example.net", 0, "admin", "pw").Port; got != 5432 {
		t.Errorf("port = %d, want 5432", got)
	}
	if got := External("c", "mysql", "db.example.net", 0, "admin", "pw").Port; got != 3306 {
		t.Errorf("port = %d, want 3306", got)
	}
}
