package dbcred

import (
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/dbconn"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// A site's account is named after its database, because an operator reading the
// engine's user list has to be able to tell whose account they are looking at.
func TestUserName_IsTheDatabaseHandle(t *testing.T) {
	if got := UserName("acme_supply"); got != "acme_supply" {
		t.Errorf("UserName = %q, want the database handle", got)
	}
}

// MySQL truncates a user name over 32 characters, which would silently collapse
// two long sites onto one account, and therefore onto each other's grants.
func TestUserName_LongHandlesStayDistinct(t *testing.T) {
	a := UserName(strings.Repeat("a", 40) + "_one")
	b := UserName(strings.Repeat("a", 40) + "_two")
	if len(a) > 32 || len(b) > 32 {
		t.Fatalf("names must fit MySQL's 32-character limit, got %d and %d", len(a), len(b))
	}
	if a == b {
		t.Errorf("two sites collapsed onto one account name %q", a)
	}
}

// The name is spliced into SQL servlo composes, so it must never carry a
// character that could end the identifier.
func TestUserName_SanitisesTheHandle(t *testing.T) {
	got := UserName("Acme-Supply.co'; DROP")
	if !ValidUser(got) {
		t.Errorf("UserName = %q, which is not a safe identifier", got)
	}
}

func TestGeneratePassword_IsStrongAndSpliceSafe(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		pw, err := GeneratePassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(pw) < 24 {
			t.Errorf("password %q is only %d characters", pw, len(pw))
		}
		if !ValidPassword(pw) {
			t.Errorf("password %q carries a character that cannot be spliced into SQL", pw)
		}
		if seen[pw] {
			t.Fatalf("password %q generated twice", pw)
		}
		seen[pw] = true
	}
}

// A local connection resolved without a registry entry carries no name, and its
// accounts still have to be found again on the next run.
func TestKey_FallsBackToTheService(t *testing.T) {
	if got := Key(dbconn.Connection{Service: "mysql"}); got != "mysql" {
		t.Errorf("Key = %q, want the service standing in for the unnamed connection", got)
	}
	if got := Key(dbconn.Connection{Name: "do-managed", Service: ""}); got != "do-managed" {
		t.Errorf("Key = %q, want the connection's name", got)
	}
}

func TestRecordAndFor_RoundTrip(t *testing.T) {
	isolate(t)

	conn := dbconn.Connection{Service: "mysql", Family: "mysql"}
	password, err := GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	if err := Record(conn, "acme", "acme", password); err != nil {
		t.Fatal(err)
	}

	got, ok := For(conn, "acme")
	if !ok {
		t.Fatal("the account did not survive a round trip")
	}
	if got.User != "acme" || got.Password != password {
		t.Errorf("read back %+v", got)
	}
}

// A site on two connections has two accounts. Keying by database alone would
// hand the managed server's password to the local one.
func TestFor_KeysByConnection(t *testing.T) {
	isolate(t)

	local := dbconn.Connection{Service: "mysql", Family: "mysql"}
	managed := dbconn.Connection{Name: "do-managed", Family: "mysql", Host: "db.example.com", Port: 3306}
	localPass, _ := GeneratePassword()
	managedPass, _ := GeneratePassword()
	if err := Record(local, "acme", "acme", localPass); err != nil {
		t.Fatal(err)
	}
	if err := Record(managed, "acme", "acme", managedPass); err != nil {
		t.Fatal(err)
	}

	gotLocal, _ := For(local, "acme")
	gotManaged, _ := For(managed, "acme")
	if gotLocal.Password == gotManaged.Password {
		t.Error("two connections resolved to one stored password")
	}
	if gotLocal.Password != localPass || gotManaged.Password != managedPass {
		t.Errorf("accounts crossed over: local %q managed %q", gotLocal.Password, gotManaged.Password)
	}
}

// A site that has never been provisioned is the ordinary state of every site
// that existed before per-site accounts did, so it is a miss rather than an
// error, and the caller falls back to the connection's administrator.
func TestFor_MissingSiteIsNotAnError(t *testing.T) {
	isolate(t)

	if _, ok := For(dbconn.Connection{Service: "mysql"}, "never-provisioned"); ok {
		t.Error("reported an account for a site that has none")
	}
}

// A recorded row whose user name could not have come from here is not one to
// splice into a statement, whatever put it in the file.
func TestFor_RejectsAnUnusableRecordedName(t *testing.T) {
	isolate(t)

	conn := dbconn.Connection{Service: "mysql"}
	if err := dbconn.RecordSiteUser("mysql", "acme", "root'; DROP USER x; --", "irrelevant"); err != nil {
		t.Fatal(err)
	}
	if _, ok := For(conn, "acme"); ok {
		t.Error("accepted a recorded account name that is not a safe identifier")
	}
}
