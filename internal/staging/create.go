package staging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/siteops"
)

// Options is what to make.
type Options struct {
	// Origin is the live site, by name or by any of its domains.
	Origin string
	// Domain is where the staging site is served. Given rather than derived:
	// servlo appends no suffix to anything (CLAUDE.md §3.3), and guessing
	// staging.<origin> would be wrong for anybody whose staging lives on a
	// different domain entirely.
	Domain string
	// Path is where the files go. Empty puts it beside the live site, named
	// after its own domain.
	Path string
	// User is who logs in. Empty uses the default.
	User string
}

// Created is the new site and the one-time credentials for it.
type Created struct {
	Site        config.Site
	Credentials Credentials
	// Note is anything true and worth saying that is not a failure.
	Note string
}

// Create makes a staging copy of a live site.
//
// It creates the site and nothing else: an empty directory, its own database
// connection setting, and the two guards. Filling it is Refresh's job, kept
// separate because "make a staging site" and "put today's live data on it" are
// two decisions, and the second one is the one somebody will want to repeat.
func Create(opts Options) (Created, error) {
	origin, err := config.FindSiteByRef(strings.TrimSpace(opts.Origin))
	if err != nil {
		return Created{}, fmt.Errorf("%q is not a site on this server", opts.Origin)
	}
	// No staging of staging. The refresh chain that would imply is a copy of a
	// copy, and the question "is this data live" stops having an answer.
	if origin.IsStaging() {
		return Created{}, fmt.Errorf("%s is itself a staging site, so it cannot be the origin of another", origin.Name)
	}

	domain, err := siteops.NormalizeDomain(opts.Domain)
	if err != nil {
		return Created{}, err
	}
	if origin.HasDomain(domain) {
		return Created{}, fmt.Errorf("%s is already a domain of the live site. A staging site needs a domain of its own", domain)
	}
	if existing, err := config.FindSiteByDomain(domain); err == nil {
		return Created{}, fmt.Errorf("%s is already served by %s", domain, existing.Name)
	}

	path := strings.TrimSpace(opts.Path)
	if path == "" {
		path = filepath.Join(filepath.Dir(origin.Path), domain)
	}
	if path == origin.Path {
		// The one mistake that would destroy the live site on the first
		// refresh, so it is refused here rather than there.
		return Created{}, fmt.Errorf("a staging site cannot live in the same directory as the site it copies")
	}
	if entries, err := os.ReadDir(path); err == nil && len(entries) > 0 {
		return Created{}, fmt.Errorf("%s already has something in it. Name an empty directory, or move that aside first", path)
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return Created{}, err
	}

	creds, hash, err := NewCredentials(opts.User)
	if err != nil {
		return Created{}, err
	}

	site := config.Site{
		Name:    siteops.SiteName(domain),
		Domains: []string{domain},
		Path:    path,
		// Everything about how the live site runs, so staging is a copy in the
		// sense that matters: same PHP, same framework, same document root. A
		// staging site on a different PHP version tests a different thing from
		// the one about to go live.
		PHPVersion:  origin.PHPVersion,
		NodeVersion: origin.NodeVersion,
		Framework:   origin.Framework,
		PublicDir:   origin.PublicDir,
		Database:    origin.Database,
		Staging:     &config.SiteStaging{Origin: origin.Name, User: creds.User, Hash: hash},
	}

	if err := WriteHtpasswd(domain, creds.User, hash); err != nil {
		return Created{}, fmt.Errorf("writing the staging credentials: %w", err)
	}
	if err := siteops.FinishLink(site, site.PHPVersion); err != nil {
		RemoveHtpasswd(domain)
		return Created{}, err
	}

	out := Created{Site: site, Credentials: creds}
	if origin.Database == "" {
		out.Note = "The live site names no database connection, so this one has none either. " +
			"Give it one before refreshing, or the refresh will copy files and nothing else."
	}
	return out, nil
}

// Remove takes the staging marks off a site: the credentials file, and the
// block that made it staging.
//
// Deleting the site itself is site removal, which already exists and already
// asks for the name to be typed. This is the narrower thing: a staging site
// that is being promoted, or one whose guards somebody wants gone.
func Remove(site *config.Site) error {
	if !site.IsStaging() {
		return fmt.Errorf("%s is not a staging site", site.Name)
	}
	site.Staging = nil
	if err := config.AddSite(*site); err != nil {
		return err
	}
	RemoveHtpasswd(site.PrimaryDomain())
	return nil
}
