package dbconn

import (
	"os"
	"strings"
	"testing"
)

// The password servlo generated is the password the site's env file carries, so
// it has to survive the run that generated it. Without it, the next `servlo env`
// finds the user already there and has nothing to write.
func TestSiteUser_RoundTrips(t *testing.T) {
	isolate(t)

	if err := RecordSiteUser("managed", "shop", "shop", "generated-pw"); err != nil {
		t.Fatal(err)
	}
	got, ok := SiteUserFor("managed", "shop")
	if !ok {
		t.Fatal("the site user was not recorded")
	}
	if got.User != "shop" || got.Password != "generated-pw" {
		t.Errorf("site user = %+v, want the one recorded", got)
	}
	if _, ok := SiteUserFor("managed", "other"); ok {
		t.Error("a database that was never provisioned has a user")
	}
	if _, ok := SiteUserFor("other", "shop"); ok {
		t.Error("the same database name on another connection resolved to this one")
	}
}

// Recording twice replaces rather than appends: two rows for one database
// resolve by iteration order, which is the sort of thing that works until the
// day it does not.
func TestRecordSiteUser_ReplacesTheEntryForTheSameDatabase(t *testing.T) {
	isolate(t)

	if err := RecordSiteUser("managed", "shop", "shop", "first"); err != nil {
		t.Fatal(err)
	}
	if err := RecordSiteUser("managed", "shop", "shop", "second"); err != nil {
		t.Fatal(err)
	}
	file, err := readSiteUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Users) != 1 {
		t.Fatalf("%d users recorded, want 1", len(file.Users))
	}
	if file.Users[0].Password != "second" {
		t.Errorf("password = %q, want the one recorded last", file.Users[0].Password)
	}
}

// Every site on this machine runs as the same Linux user, so a mode of 0644
// here would put every site's database password inside reach of all of them.
func TestSiteUsers_AreUnreadableByAnybodyElse(t *testing.T) {
	isolate(t)

	if err := RecordSiteUser("managed", "shop", "shop", "generated-pw"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(siteUsersPath())
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("mode = %04o, want 0600", mode)
	}
}

// Removing a connection takes the accounts on it. Left behind they are
// credentials for a server nothing points at, and the next connection to take
// that name would inherit them.
func TestForgetSiteUsers_DropsOnlyThatConnections(t *testing.T) {
	isolate(t)

	if err := RecordSiteUser("managed", "shop", "shop", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := RecordSiteUser("other", "blog", "blog", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := ForgetSiteUsers("managed"); err != nil {
		t.Fatal(err)
	}
	if _, ok := SiteUserFor("managed", "shop"); ok {
		t.Error("the removed connection's account is still recorded")
	}
	if _, ok := SiteUserFor("other", "blog"); !ok {
		t.Error("another connection's account went with it")
	}
	if err := ForgetSiteUsers("nothing-here"); err != nil {
		t.Errorf("forgetting a connection with no accounts failed: %v", err)
	}
}

// A site user's name is derived from its database, and a database name too long
// for MySQL's 32-character limit still has to come out unique: two sites
// sharing one account is two sites sharing one set of rights.
func TestSiteUserName_StaysWithinTheLimitAndStaysUnique(t *testing.T) {
	if got := SiteUserName("shop"); got != "shop" {
		t.Errorf("SiteUserName(shop) = %q, want it left alone", got)
	}

	a := SiteUserName(strings.Repeat("a", 40) + "_one")
	b := SiteUserName(strings.Repeat("a", 40) + "_two")
	for _, name := range []string{a, b} {
		if len(name) > 32 {
			t.Errorf("%q is %d characters, longer than MySQL accepts", name, len(name))
		}
	}
	if a == b {
		t.Errorf("two databases were given the same user name: %q", a)
	}
}
