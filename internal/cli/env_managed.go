package cli

import (
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbcred"
)

// Giving a site on a managed database something to connect with.
//
// A database servlo runs is provisioned inside its container: the engine's
// admin is a container away and the site's env file carries it. A managed
// database is neither. Servlo has to open it over the network, create the
// site's schema, and create an account with rights to that schema and nothing
// else, because the alternative is writing the provider's administrator into
// every site's env file and giving each site every other site's data.

// provisionManaged is the network reach, a seam so the wiring around it can be
// driven through each outcome without a managed database.
var provisionManaged = dbconn.CreateDatabaseAndUser

// managedSiteCredentials creates the site's database and its own user on a
// managed connection, and returns what the env file should carry for them.
//
// An empty user and password with no error is the one case servlo cannot answer
// for: the account is already on the server and servlo did not create it, so it
// does not hold the password. The caller leaves the env file's database
// credentials as they are rather than writing a blank one over a working site.
func managedSiteCredentials(c dbconn.Connection, database string) (user, password string, err error) {
	// dbcred names the account on the local path too. A site that moves between
	// a managed database and one servlo runs has to arrive at the same account
	// on both, so the name comes from one place rather than being derived twice.
	user = dbcred.UserName(database)
	password, err = provisionManaged(c, database, user)
	if err != nil {
		return "", "", err
	}
	if password == "" {
		return "", "", nil
	}
	return user, password, nil
}
