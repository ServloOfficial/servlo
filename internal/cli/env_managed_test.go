package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbcred"
)

func stubProvisionManaged(t *testing.T, password string, err error) *[]string {
	t.Helper()
	var seen []string
	prev := provisionManaged
	t.Cleanup(func() { provisionManaged = prev })
	provisionManaged = func(_ dbconn.Connection, database, username string) (string, error) {
		seen = append(seen, database+"/"+username)
		return password, err
	}
	return &seen
}

// A site on a managed database gets its own account, not the provider's
// administrator. Writing doadmin into every site's env file would give each
// site every other site's data on the same cluster.
func TestManagedSiteCredentials_GivesTheSiteItsOwnAccount(t *testing.T) {
	seen := stubProvisionManaged(t, "generated-pw", nil)
	c := dbconn.External("managed", "postgres", "db.example.net", 25060, "doadmin", "admin-pw")

	user, password, err := managedSiteCredentials(c, "shop")
	if err != nil {
		t.Fatal(err)
	}
	if user != "shop" || password != "generated-pw" {
		t.Errorf("credentials = %q/%q, want the site's own account", user, password)
	}
	if password == "admin-pw" {
		t.Error("the site was handed the connection's administrative password")
	}
	if len(*seen) != 1 || (*seen)[0] != "shop/shop" {
		t.Errorf("provisioned %v, want the site's database and user", *seen)
	}
}

// An account that is already there and was not created here is the one case
// servlo cannot answer for. Writing a blank password over a working site is
// worse than leaving the env file as it is.
func TestManagedSiteCredentials_LeavesTheEnvAloneWhenThePasswordIsUnknown(t *testing.T) {
	stubProvisionManaged(t, "", nil)
	c := dbconn.External("managed", "mysql", "db.example.net", 3306, "admin", "pw")

	user, password, err := managedSiteCredentials(c, "shop")
	if err != nil {
		t.Fatal(err)
	}
	if user != "" || password != "" {
		t.Errorf("credentials = %q/%q, want nothing to write", user, password)
	}
}

// A database name too long for MySQL's account limit still gets an account, and
// a distinct one.
func TestManagedSiteCredentials_DerivesAnAccountNameThatFits(t *testing.T) {
	seen := stubProvisionManaged(t, "pw", nil)
	c := dbconn.External("managed", "mysql", "db.example.net", 3306, "admin", "pw")

	long := strings.Repeat("a", 40) + "_site"
	user, _, err := managedSiteCredentials(c, long)
	if err != nil {
		t.Fatal(err)
	}
	if len(user) > 32 {
		t.Errorf("user %q is %d characters, longer than MySQL accepts", user, len(user))
	}
	if len(*seen) != 1 || !strings.HasPrefix((*seen)[0], long+"/") {
		t.Errorf("provisioned %v, want the full database name with the shortened user", *seen)
	}
}

// The account name a managed database gets is the one a local database would
// have given the same site. A site that moves between the two must not arrive
// with a second account, and a handle no engine would accept as a bare
// identifier has to be turned into one on both paths, not just the local one.
func TestManagedSiteCredentials_NamesTheAccountTheWayTheLocalPathDoes(t *testing.T) {
	seen := stubProvisionManaged(t, "pw", nil)
	c := dbconn.External("managed", "mysql", "db.example.net", 3306, "admin", "pw")

	user, _, err := managedSiteCredentials(c, "1shop")
	if err != nil {
		t.Fatal(err)
	}
	if want := dbcred.UserName("1shop"); user != want {
		t.Errorf("user = %q, want %q, the name the local path issues", user, want)
	}
	if len(*seen) != 1 || (*seen)[0] != "1shop/"+dbcred.UserName("1shop") {
		t.Errorf("provisioned %v, want the account named as dbcred names it", *seen)
	}
}

// A managed database that refuses says why, and the reason travels rather than
// being flattened into "could not create database".
func TestManagedSiteCredentials_CarriesTheFailureUp(t *testing.T) {
	stubProvisionManaged(t, "", errors.New("cannot reach db.example.net:25060: connection refused. If this is a managed database, add this server's public IP to the provider's trusted sources"))
	c := dbconn.External("managed", "mysql", "db.example.net", 25060, "admin", "pw")

	_, _, err := managedSiteCredentials(c, "shop")
	if err == nil {
		t.Fatal("a failed provision reported success")
	}
	if !strings.Contains(err.Error(), "trusted sources") {
		t.Errorf("error = %q, does not carry what went wrong", err)
	}
}
