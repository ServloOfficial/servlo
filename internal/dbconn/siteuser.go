package dbconn

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/realrashid/servlo/internal/config"
)

// The account a site holds on a managed database.
//
// Servlo generates the password, so servlo is the only thing that can write it
// into the site's env file on the next run. Without it written down, a second
// `servlo env` would find the user already there, have nothing to put in
// DB_PASSWORD, and leave the site with an env file it cannot connect with. The
// alternative, generating a new password each run and forcing it onto the
// server, takes every site on that database down for as long as it takes
// somebody to notice.
//
// It sits beside the connection registry, in the config directory and
// owner-only, because it is the same kind of secret in the same threat model:
// every site on this machine runs as one Linux user, so a database credential
// readable by that user's world is a credential readable by every site.

// SiteUser is one site's account on one connection.
type SiteUser struct {
	Connection string `yaml:"connection"`
	Database   string `yaml:"database"`
	User       string `yaml:"user"`
	Password   string `yaml:"password"`
}

type siteUserFile struct {
	Users []SiteUser `yaml:"users,omitempty"`
}

func siteUsersPath() string { return filepath.Join(config.ConfigDir(), "database-users.yaml") }

func readSiteUsers() (siteUserFile, error) {
	var out siteUserFile
	data, err := os.ReadFile(siteUsersPath())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, fmt.Errorf("reading the database users: %w", err)
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("parsing the database users: %w", err)
	}
	return out, nil
}

func writeSiteUsers(file siteUserFile) error {
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.ConfigDir(), 0700); err != nil {
		return err
	}
	// Written through a temp file created 0600, so a site password is never on
	// disk at a wider mode even for an instant.
	tmp := siteUsersPath() + ".new"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, siteUsersPath())
}

// SiteUserFor returns the account servlo created for a database on a
// connection, and whether there is one.
func SiteUserFor(connection, database string) (SiteUser, bool) {
	file, err := readSiteUsers()
	if err != nil {
		return SiteUser{}, false
	}
	for _, u := range file.Users {
		if u.Connection == connection && u.Database == database {
			return u, true
		}
	}
	return SiteUser{}, false
}

// RecordSiteUser writes one down, replacing any entry for the same database so
// a re-provision cannot leave two rows that resolve by iteration order.
func RecordSiteUser(connection, database, user, password string) error {
	file, err := readSiteUsers()
	if err != nil {
		return err
	}
	for i, existing := range file.Users {
		if existing.Connection == connection && existing.Database == database {
			file.Users[i] = SiteUser{Connection: connection, Database: database, User: user, Password: password}
			return writeSiteUsers(file)
		}
	}
	file.Users = append(file.Users, SiteUser{Connection: connection, Database: database, User: user, Password: password})
	return writeSiteUsers(file)
}

// ForgetSiteUsers drops every account on a connection, for when the connection
// itself is removed. Left behind they are credentials for a server nothing
// points at, and the next connection to take that name would inherit them.
func ForgetSiteUsers(connection string) error {
	file, err := readSiteUsers()
	if err != nil {
		return err
	}
	var kept []SiteUser
	for _, u := range file.Users {
		if u.Connection != connection {
			kept = append(kept, u)
		}
	}
	if len(kept) == len(file.Users) {
		return nil
	}
	file.Users = kept
	return writeSiteUsers(file)
}

// SiteUserName is the account name a site's database gets.
//
// It is the database name where that fits. MySQL stops at 32 characters, and a
// plain truncation would give two long-named sites the same account and
// therefore each other's rights, so what is cut is replaced by a digest of the
// whole name.
func SiteUserName(database string) string {
	if len(database) <= 32 {
		return database
	}
	sum := sha256.Sum256([]byte(database))
	return database[:24] + "_" + hex.EncodeToString(sum[:])[:7]
}
