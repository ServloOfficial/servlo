// Package appinstall turns an app definition into a serving site.
//
// The engine in internal/appstore stops deliberately short: it fetches and
// verifies a release, creates a database if the definition asks for one, and
// writes the config file. What it cannot do from there is register a site or
// drive an application's own setup form, because both of those need the site to
// exist and answer requests. This is the part that knows how.
//
// It is its own package rather than a function in the CLI because the panel
// needs the same steps in the same order, and the order is the design: each
// step is undoable only by the step that has not happened yet.
//
// Nothing here knows what any app is (CLAUDE.md §2). It reads a definition and
// does what it says.
package appinstall

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ServloOfficial/servlo/internal/appstore"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbcred"
	"github.com/ServloOfficial/servlo/internal/dbuser"
	phpDet "github.com/ServloOfficial/servlo/internal/php"
	"github.com/ServloOfficial/servlo/internal/serviceops"
	"github.com/ServloOfficial/servlo/internal/siteops"
)

// Options is what the operator asked for.
type Options struct {
	// App is the name in the app store.
	App string
	// Domain is where the site is served. Given, never derived: servlo appends
	// no TLD to anything (CLAUDE.md §3.3).
	Domain string
	// Path is the directory the application is unpacked into. It must be empty
	// or absent.
	Path string
	// Connection names the database connection to use. Empty takes the default,
	// which may equally be a local container or a managed server somewhere else
	// (PRD §5.9).
	Connection string
	// AdminUser, AdminEmail and SiteTitle fill the application's own setup form
	// for the apps that have one. The password is generated, never given.
	AdminUser  string
	AdminEmail string
	SiteTitle  string
}

// Installed is what the install produced, including the one-time credentials.
type Installed struct {
	Site config.Site
	App  appstore.App
	// Database is the account the application will connect as. Its password is
	// in here because the caller has to show it once; nothing logs this.
	Database appstore.Connection
	// AdminUser and AdminPassword are the account the setup form created. Both
	// empty when the app declares no setup, which is not a failure: see Note.
	AdminUser     string
	AdminPassword string
	// Note is what is left for the operator to do, for an app whose own
	// installer servlo cannot drive. Empty when the install finished the job.
	Note string
}

// The fallible steps, as seams. Every one of them needs a container, a network
// or a registry, so a test that could not replace them could only be run on a
// server.
var (
	loadApp        = appstore.Load
	namedConn      = dbconn.Named
	defaultConn    = dbconn.Default
	createDatabase = serviceops.CreateDatabase
	ensureDBUser   = dbuser.Ensure
	provisionRemat = dbconn.CreateDatabaseAndUser
	fetchAndWrite  = func(ctx context.Context, app appstore.App, req appstore.Request, deps appstore.Deps) (appstore.Result, error) {
		return app.Install(ctx, req, deps)
	}
	registerSite = siteops.FinishLink
	// waitForSiteFn blocks until the new site answers. A seam so a test does
	// not have to serve HTTP to install anything.
	waitForSiteFn = waitForSite
	// detectPHPVersion resolves the version the site will run on. A seam so a
	// test can name one without a PHP installation to detect.
	detectPHPVersion = phpDet.DetectVersion
	runSetup         = func(ctx context.Context, app appstore.App, siteURL string, values map[string]string) error {
		return app.Setup.Run(ctx, siteURL, values)
	}
	generatePassword = dbcred.GeneratePassword
)

// Install carries out the whole thing, in the order that leaves the least
// behind when a step fails.
func Install(ctx context.Context, opts Options) (Installed, error) {
	var out Installed

	app, err := loadApp(strings.TrimSpace(opts.App))
	if err != nil {
		return out, err
	}
	out.App = app

	domain, err := siteops.NormalizeDomain(opts.Domain)
	if err != nil {
		return out, err
	}
	if existing, err := config.FindSiteByDomain(domain); err == nil {
		return out, fmt.Errorf("%s is already served by %s", domain, existing.Name)
	}

	path, err := prepareDir(opts.Path, domain)
	if err != nil {
		return out, err
	}

	siteName := siteops.SiteName(domain)
	siteURL := "http://" + domain

	var conn dbconn.Connection
	if app.Database.Required {
		conn, err = resolveConnection(opts.Connection)
		if err != nil {
			return out, err
		}
	}

	deps := appstore.Deps{}
	if app.Database.Required {
		deps.CreateDatabase = func(name string) (appstore.Connection, error) {
			return provision(conn, name)
		}
	}

	res, err := fetchAndWrite(ctx, app, appstore.Request{
		Dir:          path,
		SiteURL:      siteURL,
		DatabaseName: config.SiteSlug(siteName),
	}, deps)
	if err != nil {
		return out, err
	}
	out.Database = res.Connection

	// Resolved rather than left empty, and this is the whole of that bug: the
	// version names the container, so an empty one sent the site's pool to
	// fpm-pools/servlo-php-fpm while the container that runs mounts
	// fpm-pools/servlo-php85-fpm. The pool was written somewhere nothing reads,
	// the vhost found none and so wired no PHP upstream, and nginx answered the
	// POST that drives the application's own installer by serving install.php
	// as a static file: 405. It also wrote a quadlet for a container that
	// cannot exist, left behind as a dead unit called "Servlo PHP  FPM".
	//
	// Detection reads the release servlo just extracted, so an application that
	// pins a version in its own project file gets it; anything else falls back
	// to the machine default, which is what a fresh droplet has.
	// The document root, from the release that was just extracted. The site
	// literal named neither this nor the PHP version, and the linker path names
	// both — an omission is not a default here, it is a field every consumer
	// reads straight off the registry entry.
	//
	// A framework's own definition supplies the root when nginx builds a vhost,
	// so a site missing it may still serve; what it cannot do is tell the
	// panel, the deploy or the doctor where its code lives.
	publicDir := "."
	if report, rerr := siteops.InspectSiteDirectory(path); rerr == nil && report.PublicDir != "" {
		publicDir = report.PublicDir
	}

	phpVersion, err := detectPHPVersion(path)
	if err != nil || phpVersion == "" {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil || cfg.PHP.DefaultVersion == "" {
			return out, fmt.Errorf("cannot tell which PHP version %s should run on, and a site registered without one gets a pool and a quadlet named after a container that does not exist", domain)
		}
		phpVersion = cfg.PHP.DefaultVersion
	}

	site := config.Site{
		Name:       siteName,
		Domains:    []string{domain},
		Path:       path,
		Framework:  app.Framework,
		PHPVersion: phpVersion,
		PublicDir:  publicDir,
	}
	// The registry entry first, then the artifacts. `servlo link` registers a
	// site in linker.Apply, a layer this path does not go through, and calling
	// FinishLink alone writes the pool, the vhost and the quadlet without ever
	// adding the site to sites.yaml. An install used to leave a site nginx
	// served and nothing else knew about: absent from `servlo sites` and the
	// panel, skipped by every feature that walks the registry, so no backups,
	// no scheduled cron, and `servlo secure` unable to find the domain.
	//
	// In this order because FinishLink's own steps read the registry back.
	if err := config.AddSite(site); err != nil {
		return out, fmt.Errorf("registering site: %w", err)
	}
	if err := registerSite(site, site.PHPVersion); err != nil {
		return out, err
	}
	out.Site = site

	if !app.Setup.Declared() {
		// Not a failure and not silence either. An application whose own
		// installer servlo cannot drive is one the operator has to finish, and
		// an install that did not say so leaves an uninstalled application on a
		// live domain for the first passer-by to claim.
		out.Note = fmt.Sprintf("%s finishes in its own installer. Open %s and complete it before pointing DNS at this domain.", app.Label, siteURL)
		return out, nil
	}

	// The site has to be answering before its installer can be driven, and it
	// was not waited for. See waitForSite: the window between writing the vhost
	// and nginx serving it is where a POST used to land, and the install then
	// reported a setup that did not complete over an application that was
	// perfectly fine a second later.
	if err := waitForSiteFn(ctx, siteURL); err != nil {
		return out, fmt.Errorf("%s is installed and serving, but its setup could not be driven: %w", app.Label, err)
	}

	password, err := generatePassword()
	if err != nil {
		return out, err
	}
	values := map[string]string{
		"site_title":     firstNonEmpty(opts.SiteTitle, domain),
		"admin_user":     firstNonEmpty(opts.AdminUser, "admin"),
		"admin_password": password,
		"admin_email":    opts.AdminEmail,
		"site_url":       siteURL,
	}
	if err := runSetup(ctx, app, siteURL, values); err != nil {
		// The site is registered and serving, so this is reported rather than
		// rolled back: the operator can finish the form themselves, and taking
		// the site away would lose the release and the database with it.
		return out, fmt.Errorf("%s is installed and serving, but its setup did not complete: %w", app.Label, err)
	}
	out.AdminUser = values["admin_user"]
	out.AdminPassword = password
	return out, nil
}

// prepareDir resolves the site directory and refuses one with anything in it.
//
// Refused before the download rather than after it, so a mistake costs a
// message instead of sixty megabytes and a half-populated directory.
func prepareDir(path, domain string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = filepath.Join(cwd, domain)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if entries, err := os.ReadDir(abs); err == nil && len(entries) > 0 {
		return "", fmt.Errorf("%s already has something in it. Name an empty directory, or move that aside first", abs)
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return "", err
	}
	return abs, nil
}

// resolveConnection returns the connection to provision on, by name or the
// default.
func resolveConnection(name string) (dbconn.Connection, error) {
	if strings.TrimSpace(name) != "" {
		return namedConn(name)
	}
	return defaultConn()
}

// provision creates the database and the account that reaches it, and returns
// what the config file should point at.
//
// The two halves of PRD §5.9 arrive at the same answer by different routes. A
// database servlo hosts is created inside its container and its account issued
// by the store-declared statements; one it does not host is opened over the
// network, because the alternative is writing the provider's administrator into
// the site's config file.
func provision(conn dbconn.Connection, database string) (appstore.Connection, error) {
	out := appstore.Connection{Name: database, Host: dbuser.Address(conn)}

	if !conn.Local() {
		user := dbcred.UserName(database)
		password, err := provisionRemat(conn, database, user)
		if err != nil {
			return appstore.Connection{}, err
		}
		if password == "" {
			// The account is on the server and servlo did not create it, so it
			// does not hold the password. Writing a blank one into a config file
			// is the silent-wrong-value bug the renderer already refuses; say so
			// here instead, where the cause is known.
			return appstore.Connection{}, fmt.Errorf("the account %s already exists on %s and servlo did not create it, so it cannot give the application a password for it", user, conn.Name)
		}
		out.User, out.Password = user, password
		return out, nil
	}

	if _, err := createDatabase(conn.Service, database); err != nil {
		return appstore.Connection{}, err
	}
	su, err := ensureDBUser(conn, database, nil)
	if err != nil {
		if dbuser.Unsupported(err) {
			return appstore.Connection{}, fmt.Errorf("%s issues no per-site database accounts, so this app has no credentials to connect with", conn.Service)
		}
		return appstore.Connection{}, err
	}
	out.User, out.Password = su.User, su.Password
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
