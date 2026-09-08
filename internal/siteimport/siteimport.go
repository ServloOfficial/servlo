// Package siteimport takes a site that is already running somewhere else and
// makes it a site here.
//
// The inputs are what somebody actually has when they are moving off shared
// hosting or off another panel: a directory of files, and a .sql dump. Not a
// git remote, not a backup archive in servlo's own format, not an API on the
// old host. Those exist and are already handled; this is for the case where all
// anybody could get out of the old place was a tarball and a dump.
//
// What servlo adds is everything around them: the framework detected, the
// document root found, a database created with an account of its own, the dump
// loaded into it, the .env pointed at it, the vhost written.
package siteimport

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbdump"
	"github.com/ServloOfficial/servlo/internal/siteops"
	"github.com/ServloOfficial/servlo/internal/sitetpl"
)

// Options is what to import.
type Options struct {
	// Path is the directory the files are already in. Servlo does not move
	// them: a site directory somebody has just uploaded a few gigabytes into is
	// not one to copy again for no reason.
	Path string
	// Domain is where it will be served.
	Domain string
	// Dump is a .sql file to load, empty for a site whose data is coming from
	// somewhere else or is not coming at all.
	Dump string
	// Connection names which database connection to put it on, empty for the
	// server's default.
	Connection string
	// PHPVersion overrides what servlo would pick.
	PHPVersion string
}

// Result is what was imported and what servlo worked out about it.
type Result struct {
	Site config.Site
	// Framework is what the files look like, empty when servlo could not tell.
	Framework string
	// PublicDir is the document root it found.
	PublicDir string
	// Database is what the dump was loaded into, empty when there was no dump.
	Database string
	// Notes are the things worth saying that are not failures: what servlo
	// guessed, and what it could not do for you.
	Notes []string
}

// loadDump is the seam. Loading a dump needs a database container, which a test
// has no business starting.
// finishLink writes the site's artifacts. A seam, because they need podman,
// systemd and a running nginx, and a test has none of those.
var finishLink = siteops.FinishLink

var loadDump = func(conn dbconn.Connection, database string, r io.Reader) error {
	return dbdump.Load(conn, database, r)
}

// Import registers a directory as a site and loads a dump into its database.
func Import(opts Options) (Result, error) {
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return Result{}, fmt.Errorf("which directory? Name the one the files are already in")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("%s is not a directory on this server. Upload the files first, then import them", path)
	}

	domain, err := siteops.NormalizeDomain(opts.Domain)
	if err != nil {
		return Result{}, err
	}
	if existing, err := config.FindSiteByDomain(domain); err == nil {
		return Result{}, fmt.Errorf("%s is already served by %s", domain, existing.Name)
	}

	// Checked before anything is registered. A dump that is not there, or is
	// not a dump, is worth finding out before a half-imported site exists.
	if opts.Dump != "" {
		if err := readableDump(opts.Dump); err != nil {
			return Result{}, err
		}
	}

	report, err := siteops.InspectSiteDirectory(abs)
	if err != nil {
		return Result{}, fmt.Errorf("looking at %s: %w", abs, err)
	}

	out := Result{Framework: report.Framework, PublicDir: report.PublicDir}
	site := config.Site{
		Name:       siteops.SiteName(domain),
		Domains:    []string{domain},
		Path:       abs,
		Framework:  report.Framework,
		PublicDir:  report.PublicDir,
		PHPVersion: opts.PHPVersion,
		Database:   opts.Connection,
	}
	if site.PHPVersion == "" {
		site.PHPVersion = report.PHPVersion
	}
	if site.Framework == "" {
		out.Notes = append(out.Notes, "Servlo could not tell what framework this is, so it has no workers, "+
			"no deploy script and no health checks. Set the framework on the site's settings to get them.")
	}
	if report.PublicDir == "" || report.PublicDir == "." {
		out.Notes = append(out.Notes, "The document root is the site directory itself. If the application "+
			"serves from a subdirectory, set it before pointing DNS here, or the source will be downloadable.")
	}

	// Registered before its artifacts are written, and it was not registered at
	// all. FinishLink writes the pool, the vhost and the quadlet; adding the
	// site to sites.yaml happens a layer up in linker.Apply, which this path
	// does not go through, so an imported site was one nginx served and nothing
	// else knew about — including `servlo secure`, which this command's own
	// closing line tells the operator to run next.
	//
	// Before rather than after, because PublishLinks rewrites the container
	// hosts file from the registry, and a site missing from it is missing from
	// every container's view of which domains resolve where.
	if err := config.AddSite(site); err != nil {
		return out, fmt.Errorf("registering site: %w", err)
	}
	if err := finishLink(site, site.PHPVersion); err != nil {
		return out, err
	}
	out.Site = site

	if opts.Dump != "" {
		database, err := load(&site, opts.Dump)
		if err != nil {
			// The site exists and serves; only the data did not arrive. Saying
			// so beats unregistering a site somebody has just uploaded.
			out.Notes = append(out.Notes, "The site is registered and serving, but the dump did not load: "+err.Error())
			return out, nil
		}
		out.Database = database
	} else {
		out.Notes = append(out.Notes, "No dump was given, so the site has no data. Load one later with servlo db:import.")
	}
	return out, nil
}

// readableDump refuses what will not load, before a site is created for it.
func readableDump(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("no dump at %s", path)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory. Name the .sql file itself", path)
	}
	if info.Size() == 0 {
		return fmt.Errorf("%s is empty, so there is nothing in it to import", path)
	}
	// A compressed dump is the commonest thing somebody has and the commonest
	// thing to hand over by mistake, because the client's client tool will
	// accept the name and fail on the content.
	if ext := strings.ToLower(filepath.Ext(path)); ext == ".gz" || ext == ".zip" || ext == ".bz2" {
		return fmt.Errorf("%s is compressed. Unpack it first: servlo loads plain SQL", filepath.Base(path))
	}
	return nil
}

// load puts the dump into the site's own database.
func load(site *config.Site, dump string) (string, error) {
	database := sitetpl.DBName(site.Path)
	if database == "" {
		return "", fmt.Errorf("servlo cannot tell which database this site should use")
	}
	conn, err := dbconn.Named(site.Database)
	if err != nil {
		return "", fmt.Errorf("no database connection to load into: %w", err)
	}
	f, err := os.Open(dump)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck

	// Streamed from the file into the engine's client, so a dump larger than
	// the droplet's memory imports.
	if err := loadDump(conn, database, f); err != nil {
		return "", err
	}
	return database, nil
}
