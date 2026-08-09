package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/realrashid/servlo/internal/authz"
	"github.com/realrashid/servlo/internal/backup"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/dbdump"
)

// SiteBackupResponse is what the panel shows about a site's backups: what is
// scheduled, how much is kept, and what is actually on disk.
//
// The archives are the point. A schedule says what was asked for; the list says
// what a restore could actually use, and the two disagreeing is exactly the
// thing an operator needs to see.
type SiteBackupResponse struct {
	Schedule string         `json:"schedule,omitempty"`
	Verify   string         `json:"verify,omitempty"`
	Disabled bool           `json:"disabled"`
	Keep     backup.Policy  `json:"keep"`
	Archives []backup.Entry `json:"archives"`
	// KeyPath is where the one secret that opens these lives, so the card can
	// say to copy it somewhere else.
	KeyPath string `json:"key_path"`
}

// SiteBackupRequest changes a site's backup arrangements.
type SiteBackupRequest struct {
	// Action is "run", "schedule", "unschedule" or "verify".
	Action   string `json:"action"`
	Schedule string `json:"schedule,omitempty"`
	Verify   string `json:"verify,omitempty"`
	Keep     *struct {
		Daily   int `json:"daily"`
		Weekly  int `json:"weekly"`
		Monthly int `json:"monthly"`
	} `json:"keep,omitempty"`
}

// SiteBackupActionResponse says what happened, in the terms the card shows.
type SiteBackupActionResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Archive string `json:"archive,omitempty"`
	Files   int    `json:"files,omitempty"`
	Tables  int    `json:"tables,omitempty"`
	Pruned  int    `json:"pruned,omitempty"`
	// Note carries something true but not a failure: a site with no database,
	// or a sweep that could not free disk.
	Note string `json:"note,omitempty"`
}

// backupRoute dispatches:
//
//	GET  /api/sites/{domain}/backups  → the schedule, the retention and the archives
//	POST /api/sites/{domain}/backups  → run, verify, schedule or unschedule
func backupRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) == 0 || rest[0] != "backups" {
		return false
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, SiteActionResponse{Error: "site not found: " + domain})
		return true
	}
	switch {
	case len(rest) == 1 && r.Method == http.MethodGet:
		handleSiteBackupList(w, site)
	case len(rest) == 1 && r.Method == http.MethodPost:
		handleSiteBackupAction(w, r, site)
	default:
		http.NotFound(w, r)
	}
	return true
}

func handleSiteBackupList(w http.ResponseWriter, site *config.Site) {
	resp := SiteBackupResponse{
		Keep:     backup.SitePolicy(site),
		KeyPath:  backup.KeyPath(),
		Archives: []backup.Entry{},
	}
	if site.Backup != nil {
		resp.Schedule = site.Backup.Schedule
		resp.Verify = site.Backup.Verify
		resp.Disabled = site.Backup.Disabled
	}
	if list, err := backup.List(config.SiteBackupsDir(), config.SiteSlug(site.Name)); err == nil {
		resp.Archives = list
	}
	writeJSON(w, resp)
}

func handleSiteBackupAction(w http.ResponseWriter, r *http.Request, site *config.Site) {
	var req SiteBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, SiteBackupActionResponse{Error: "reading the request: " + err.Error()})
		return
	}
	switch req.Action {
	case "run":
		runSiteBackup(w, r, site)
	case "verify":
		verifySiteBackup(w, r, site)
	case "schedule", "unschedule":
		scheduleSiteBackup(w, r, site, req)
	default:
		writeJSON(w, SiteBackupActionResponse{Error: "unknown action " + req.Action})
	}
}

func runSiteBackup(w http.ResponseWriter, r *http.Request, site *config.Site) {
	rec, err := backup.ForSites().Run(site)
	if err != nil {
		writeJSON(w, SiteBackupActionResponse{Error: err.Error()})
		return
	}
	authz.SetAuditDetail(r, "backed up "+site.Name)
	resp := SiteBackupActionResponse{
		OK:      true,
		Archive: baseName(rec.Path),
		Files:   rec.Manifest.Files,
		Pruned:  rec.Pruned,
	}
	switch {
	case rec.PruneError != nil:
		resp.Note = "The backup was written, but clearing older ones failed: " + rec.PruneError.Error()
	case !rec.Manifest.Database:
		resp.Note = "This site has no database servlo can name, so the archive holds files only."
	}
	writeJSON(w, resp)
}

func verifySiteBackup(w http.ResponseWriter, r *http.Request, site *config.Site) {
	path, err := backup.Newest(config.SiteBackupsDir(), config.SiteSlug(site.Name))
	if err != nil {
		writeJSON(w, SiteBackupActionResponse{Error: err.Error()})
		return
	}
	res, err := verifyArchive(path, site)
	if err != nil {
		writeJSON(w, SiteBackupActionResponse{Error: err.Error()})
		return
	}
	authz.SetAuditDetail(r, "test-restored "+baseName(path))
	resp := SiteBackupActionResponse{OK: true, Archive: baseName(path), Tables: res.Tables}
	if res.Tables == 0 {
		resp.Note = "This archive holds no database, so only that it opens and is complete was checked."
	}
	writeJSON(w, resp)
}

func scheduleSiteBackup(w http.ResponseWriter, r *http.Request, site *config.Site, req SiteBackupRequest) {
	next := &config.SiteBackup{}
	if req.Action == "unschedule" {
		next = nil
	} else {
		calendar, err := backup.NormalizeSchedule(req.Schedule)
		if err != nil {
			writeJSON(w, SiteBackupActionResponse{Error: err.Error()})
			return
		}
		next.Schedule = calendar
		if req.Verify != "" {
			check, err := backup.NormalizeSchedule(req.Verify)
			if err != nil {
				writeJSON(w, SiteBackupActionResponse{Error: err.Error()})
				return
			}
			next.Verify = check
		}
		if req.Keep != nil {
			next.Keep = &config.BackupKeep{Daily: req.Keep.Daily, Weekly: req.Keep.Weekly, Monthly: req.Keep.Monthly}
		}
	}

	updated := *site
	updated.Backup = next
	if err := config.AddSite(updated); err != nil {
		writeJSON(w, SiteBackupActionResponse{Error: err.Error()})
		return
	}
	if err := backup.ApplySchedule(updated, servloBinaryPath()); err != nil {
		writeJSON(w, SiteBackupActionResponse{Error: err.Error()})
		return
	}
	if next == nil {
		authz.SetAuditDetail(r, "stopped scheduled backups for "+site.Name)
	} else {
		authz.SetAuditDetail(r, "scheduled backups for "+site.Name+" at "+next.Schedule)
	}
	writeJSON(w, SiteBackupActionResponse{OK: true})
}

// servloBinaryPath is what a scheduled unit runs. The panel and the CLI have to
// agree on it, or arming a schedule from one and then the other rewrites the
// unit to a different path.
func servloBinaryPath() string {
	if self, err := os.Executable(); err == nil {
		return self
	}
	return "servlo"
}

// verifyArchive test-restores one archive into a scratch database.
func verifyArchive(path string, site *config.Site) (dbdump.Result, error) {
	key, err := backup.Key()
	if err != nil {
		return dbdump.Result{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return dbdump.Result{}, err
	}
	defer f.Close() //nolint:errcheck

	man, err := backup.ReadManifest(f, key)
	if err != nil {
		return dbdump.Result{}, err
	}
	if !man.Database {
		// Opening it proved it decrypts and is complete, which is worth
		// reporting rather than treating as a failed check.
		return dbdump.Result{}, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return dbdump.Result{}, err
	}
	conn, err := dbconn.Named(site.Database)
	if err != nil {
		return dbdump.Result{}, err
	}

	pr, pw := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		_, err := backup.OpenDump(f, key, pw)
		_ = pw.CloseWithError(err)
		errc <- err
	}()
	res, verifyErr := dbdump.Verify(conn, dbdump.ScratchName(site.Name), pr)
	if dumpErr := <-errc; dumpErr != nil {
		return res, dumpErr
	}
	return res, verifyErr
}

func baseName(p string) string { return filepath.Base(p) }
