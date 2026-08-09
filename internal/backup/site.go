package backup

import (
	"io"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/dbcred"
	"github.com/realrashid/servlo/internal/dbdump"
	"github.com/realrashid/servlo/internal/siteops"
	"github.com/realrashid/servlo/internal/sitetpl"
	"github.com/realrashid/servlo/internal/version"
)

// ForSites is the runner the CLI and the panel both use: real excludes from the
// store, real dumps from the site's own connection, archives in the usual place.
func ForSites() Runner {
	return Runner{
		Dir:      config.SiteBackupsDir(),
		Excludes: siteops.BackupExcludes,
		Dump:     SiteDump,
		Policy:   SitePolicy,
		Version:  version.Version,
	}
}

// SitePolicy is how much history a site keeps.
//
// A site that has said nothing keeps servlo's default rather than everything.
// Keeping everything by default is how a server fills up quietly, and the
// default is generous enough that nobody loses a backup they wanted.
func SitePolicy(site *config.Site) Policy {
	if site == nil || site.Backup == nil || site.Backup.Keep == nil {
		return DefaultPolicy
	}
	k := site.Backup.Keep
	return Policy{Daily: k.Daily, Weekly: k.Weekly, Monthly: k.Monthly}
}

// SiteDump resolves how to dump this site's database.
//
// A nil function means the site has no database servlo can name, which is a
// site on SQLite or one whose env never said. That is recorded in the manifest
// rather than treated as a failure, so the files are still backed up and a
// restore knows not to expect data.
//
// The connection decides where the dump runs, not this: a local database is
// dumped inside its container and a managed one over the network, and dbdump
// picks between the engine's two declared statements.
func SiteDump(site *config.Site) (func(io.Writer) error, string, string) {
	if site == nil {
		return nil, "", ""
	}
	database := sitetpl.DBName(site.Path)
	if database == "" {
		return nil, "", ""
	}
	conn, err := dbconn.Named(site.Database)
	if err != nil {
		return nil, "", ""
	}
	return func(w io.Writer) error {
		return dbdump.Dump(conn, database, w)
	}, database, dbcred.Key(conn)
}
