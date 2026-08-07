package ui

import (
	"fmt"
	"net/http"

	"github.com/realrashid/servlo/internal/cli"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/linker"
	"github.com/realrashid/servlo/internal/siteops"
)

// Adding a site from an uploaded archive.
//
// The third source, and the only one where the bytes that decide what servlo
// writes arrive in the request itself. Everything that makes that safe lives in
// siteops.Unzip; what is here is the same order of refusals the other two
// sources use, so a rejected upload leaves the machine as it found it.

const (
	// uploadMaxRequestBytes bounds the request body. A site that does not fit
	// in a quarter of a gigabyte of compressed archive is a site to move onto
	// the server another way.
	uploadMaxRequestBytes = 256 << 20
	// uploadMaxExpandedBytes bounds what the archive may become on disk. The
	// gap over the request limit is ordinary compression, not an invitation.
	uploadMaxExpandedBytes = 1 << 30
	// uploadMaxFiles bounds the entry count. A PHP project with a vendor
	// directory runs to tens of thousands of files, so this is generous.
	uploadMaxFiles = 60000
	// uploadMemoryBytes is how much of the form is parsed in memory before
	// multipart spills the rest to a temporary file.
	uploadMemoryBytes = 16 << 20
)

// handleSiteUpload registers a site from an archive posted to
// POST /api/sites/upload as multipart/form-data.
func handleSiteUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, uploadMaxRequestBytes)
	if err := r.ParseMultipartForm(uploadMemoryBytes); err != nil {
		writeJSON(w, SiteCreateResponse{Error: "that upload could not be read: " + err.Error()})
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	// Refused before anything is created, same order as the other two sources.
	domain, err := siteops.NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}
	if owner, err := config.IsDomainUsed(domain); err == nil && owner != nil {
		writeJSON(w, SiteCreateResponse{Error: fmt.Sprintf("%s is already served by the site %q", domain, owner.Name)})
		return
	}
	path := r.FormValue("path")
	if path == "" {
		writeJSON(w, SiteCreateResponse{Error: "a site needs a directory to unpack into"})
		return
	}

	chosen, err := checkOverrides(r.FormValue("php_version"), r.FormValue("public_dir"))
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}

	file, header, err := r.FormFile("archive")
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: "no archive was uploaded"})
		return
	}
	defer file.Close()

	cfg, err := config.LoadGlobal()
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: "reading the servlo config: " + err.Error()})
		return
	}

	prepared, err := siteops.PrepareSiteDirectory(path)
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}
	// From here on the directory holds servlo's own work and nothing else, so
	// every failure takes it back rather than leaving the operator's retry to
	// be refused by servlo's leftovers.
	fail := func(msg string) {
		rollback(prepared)
		writeJSON(w, SiteCreateResponse{Error: msg})
	}

	unpacked, err := siteops.Unzip(file, header.Size, prepared.Path, siteops.UnzipOptions{
		MaxBytes: uploadMaxExpandedBytes,
		MaxFiles: uploadMaxFiles,
	})
	if err != nil {
		fail(err.Error())
		return
	}

	policy := linker.PanelPolicy(domain)
	plan, err := linker.Resolve(prepared.Path, cfg, policy)
	if err != nil {
		fail(err.Error())
		return
	}
	chosen.apply(plan)
	res, err := linker.Apply(plan, policy, cli.LinkDeps(), nil)
	if err != nil {
		fail(err.Error())
		return
	}

	resp := SiteCreateResponse{
		OK:         true,
		Domain:     res.Site.PrimaryDomain(),
		Name:       res.Site.Name,
		Path:       res.Site.Path,
		Framework:  res.Site.Framework,
		PHPVersion: res.Site.PHPVersion,
		PublicDir:  res.Site.PublicDir,
		Created:    prepared.Created,
	}
	// Worth saying, because it moves files the operator can see in the archive
	// to somewhere they are not: an archive from "download ZIP" is wrapped in a
	// directory named for the branch, and that wrapper is dropped.
	if unpacked.Unwrapped {
		resp.Warning = "the archive held everything inside one directory, which was unwrapped so the project sits at the site root"
	}
	writeJSON(w, resp)
}
