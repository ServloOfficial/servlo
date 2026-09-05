package ui

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/ServloOfficial/servlo/internal/authz"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/sitefs"
)

// The file manager's routes.
//
// Everything here is one site's directory and nothing else, which is the whole
// contract: the path arrives in a request, internal/sitefs decides whether it
// is inside the site, and no handler in this file resolves a path itself. If a
// future route needs a path, it goes through the same Root or it does not ship.
//
// These sit under /api/sites/{domain}/, so they inherit PermSite from the
// permission registry: an admin reaches every site, a developer reaches the
// ones assigned to them. Auditing is the middleware's, with the file named
// through authz.SetAuditDetail so the entry says which one.

const (
	// filesMaxEditBytes is the most the editor will load. Past this the file is
	// not something anybody is editing in a browser textarea.
	filesMaxEditBytes = 2 << 20
	// filesMaxSaveBytes bounds a save. Larger than the read ceiling, because a
	// saved file may have grown, and small enough that a runaway paste is
	// refused rather than written.
	filesMaxSaveBytes = 4 << 20
	// filesMaxUploadBytes bounds one uploaded file. A theme or a plugin archive
	// fits inside this; a database dump does not, and a database dump belongs
	// in the import flow rather than here.
	filesMaxUploadBytes = 128 << 20
	// filesUploadMemoryBytes is how much of the multipart form is parsed in
	// memory before it spills to a temporary file.
	filesUploadMemoryBytes = 8 << 20
	// filesMaxUnzipBytes and filesMaxUnzipFiles bound what an archive already
	// on disk may expand to.
	filesMaxUnzipBytes = 512 << 20
	filesMaxUnzipFiles = 40000
)

// filesResponse is the envelope every write in this file answers with, so the
// panel has one shape to read rather than one per route.
type filesResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// Path is what the action acted on, site-relative, so the panel can refresh
	// the right directory without guessing.
	Path string `json:"path,omitempty"`
}

// filesRoute dispatches /api/sites/{domain}/files and everything under it.
// It reports whether it handled the request.
func filesRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) == 0 || rest[0] != "files" {
		return false
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	root, err := sitefs.Open(site.Path)
	if err != nil {
		writeJSON(w, filesResponse{Error: "that site's directory cannot be read: " + err.Error()})
		return true
	}

	switch {
	case len(rest) == 1:
		handleFilesList(w, r, root)
	case len(rest) == 2 && rest[1] == "content":
		handleFilesContent(w, r, root)
	case len(rest) == 2 && rest[1] == "upload":
		handleFilesUpload(w, r, root)
	case len(rest) == 2 && rest[1] == "unzip":
		handleFilesUnzip(w, r, root)
	case len(rest) == 2 && rest[1] == "permissions":
		handleFilesPermissions(w, r, root)
	case len(rest) == 2 && rest[1] == "entry":
		handleFilesDelete(w, r, root)
	default:
		http.NotFound(w, r)
	}
	return true
}

// filesListResponse adds the absolute root to a listing, because an operator
// editing a live site should be able to see which directory they are in.
type filesListResponse struct {
	sitefs.Listing
	Root  string `json:"root"`
	Error string `json:"error,omitempty"`
}

func handleFilesList(w http.ResponseWriter, r *http.Request, root sitefs.Root) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	listing, err := root.List(r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, filesListResponse{Root: root.Path(), Error: pathError(err)})
		return
	}
	if listing.Entries == nil {
		listing.Entries = []sitefs.Entry{}
	}
	writeJSON(w, filesListResponse{Listing: listing, Root: root.Path()})
}

// filesSaveRequest is a file being written back from the editor.
type filesSaveRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func handleFilesContent(w http.ResponseWriter, r *http.Request, root sitefs.Root) {
	switch r.Method {
	case http.MethodGet:
		content, err := root.Read(r.URL.Query().Get("path"), filesMaxEditBytes)
		if err != nil {
			writeJSON(w, filesResponse{Error: pathError(err)})
			return
		}
		writeJSON(w, content)
	case http.MethodPut:
		var req filesSaveRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, filesMaxSaveBytes)).Decode(&req); err != nil {
			writeJSON(w, filesResponse{Error: "that save could not be read: " + err.Error()})
			return
		}
		authz.SetAuditDetail(r, "saved "+req.Path)
		if err := root.Write(req.Path, []byte(req.Content)); err != nil {
			writeJSON(w, filesResponse{Error: pathError(err)})
			return
		}
		writeJSON(w, filesResponse{OK: true, Path: req.Path})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleFilesUpload(w http.ResponseWriter, r *http.Request, root sitefs.Root) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// The ceiling is enforced twice: here, so an oversized body never reaches
	// the disk, and again in sitefs, so a caller that is not this one cannot
	// skip it.
	r.Body = http.MaxBytesReader(w, r.Body, filesMaxUploadBytes+filesUploadMemoryBytes)
	if err := r.ParseMultipartForm(filesUploadMemoryBytes); err != nil {
		writeJSON(w, filesResponse{Error: "that upload could not be read: " + err.Error()})
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	dir := r.FormValue("path")
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, filesResponse{Error: "no file was uploaded"})
		return
	}
	defer file.Close() //nolint:errcheck

	// The browser sends whatever the form said, and a form can say
	// "../../.ssh/authorized_keys". Only the base name is even considered, and
	// sitefs refuses it again if it still looks like a path.
	name := path.Base(strings.ReplaceAll(header.Filename, `\`, "/"))
	authz.SetAuditDetail(r, "uploaded "+path.Join(dir, name))
	if _, err := root.Upload(dir, name, file, filesMaxUploadBytes); err != nil {
		writeJSON(w, filesResponse{Error: pathError(err)})
		return
	}
	writeJSON(w, filesResponse{OK: true, Path: path.Join(dir, name)})
}

// filesUnzipRequest names an archive already inside the site.
type filesUnzipRequest struct {
	Path string `json:"path"`
}

// filesUnzipResponse reports what came out, so the panel can say "extracted 214
// files" rather than "done".
type filesUnzipResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Path  string `json:"path,omitempty"`
	Files int    `json:"files,omitempty"`
	Bytes int64  `json:"bytes,omitempty"`
}

func handleFilesUnzip(w http.ResponseWriter, r *http.Request, root sitefs.Root) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req filesUnzipRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, filesUnzipResponse{Error: "that request could not be read: " + err.Error()})
		return
	}
	authz.SetAuditDetail(r, "extracted "+req.Path)

	archive, err := root.ResolveExisting(req.Path)
	if err != nil {
		writeJSON(w, filesUnzipResponse{Error: pathError(err)})
		return
	}
	f, err := os.Open(archive)
	if err != nil {
		writeJSON(w, filesUnzipResponse{Error: pathError(err)})
		return
	}
	defer f.Close() //nolint:errcheck
	info, err := f.Stat()
	if err != nil {
		writeJSON(w, filesUnzipResponse{Error: err.Error()})
		return
	}

	// Into the directory the archive sits in, which is where an operator who
	// just uploaded a plugin expects it to land.
	dest := path.Dir(root.Rel(archive))
	if dest == "." {
		dest = ""
	}
	res, err := root.Unzip(dest, f, info.Size(), sitefs.UnzipLimits{
		MaxBytes: filesMaxUnzipBytes,
		MaxFiles: filesMaxUnzipFiles,
	})
	if err != nil {
		writeJSON(w, filesUnzipResponse{Error: pathError(err)})
		return
	}
	writeJSON(w, filesUnzipResponse{OK: true, Path: dest, Files: res.Files, Bytes: res.Bytes})
}

// filesPermissionsResponse is the plan or the result of applying it. One shape
// for both, so the panel shows the same table before and after.
type filesPermissionsResponse struct {
	sitefs.Plan
	Applied int      `json:"applied"`
	Failed  []string `json:"failed,omitempty"`
	OK      bool     `json:"ok"`
	Error   string   `json:"error,omitempty"`
}

func handleFilesPermissions(w http.ResponseWriter, r *http.Request, root sitefs.Root) {
	switch r.Method {
	case http.MethodGet:
		plan, err := root.PermissionPlan()
		if err != nil {
			writeJSON(w, filesPermissionsResponse{Error: err.Error()})
			return
		}
		writeJSON(w, filesPermissionsResponse{Plan: plan, OK: true})
	case http.MethodPost:
		result, err := root.ApplyPermissions()
		if err != nil {
			writeJSON(w, filesPermissionsResponse{Error: err.Error()})
			return
		}
		authz.SetAuditDetail(r, "fixed permissions on "+strconv.Itoa(result.Applied)+" paths")
		writeJSON(w, filesPermissionsResponse{
			Plan:    result.Plan,
			Applied: result.Applied,
			Failed:  result.Failed,
			OK:      true,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleFilesDelete(w http.ResponseWriter, r *http.Request, root sitefs.Root) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target := r.URL.Query().Get("path")
	authz.SetAuditDetail(r, "deleted "+target)
	if err := root.Delete(target); err != nil {
		writeJSON(w, filesResponse{Error: pathError(err)})
		return
	}
	writeJSON(w, filesResponse{OK: true, Path: target})
}

// pathError is what a refusal says to the panel.
//
// An escape gets one fixed sentence and nothing else. The alternative is an
// error that names the absolute path it refused, which hands somebody probing
// the panel a map of the filesystem one refusal at a time.
func pathError(err error) string {
	if errors.Is(err, sitefs.ErrOutsideRoot) {
		return "that path is outside the site directory"
	}
	return err.Error()
}
