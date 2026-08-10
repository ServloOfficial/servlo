package ui

import (
	"net/http"
	"time"

	"github.com/realrashid/servlo/internal/backup"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/version"
)

// Backing up the server's own state from the panel.
//
// Everything servlo knows that is not a site's files or data: the registry, the
// connections, the per-site database accounts, the settings. It is what makes a
// rebuild onto a fresh machine possible, and until now it existed only as a
// command, so an operator who never opens a terminal had every site backed up
// and nothing that could put them back together.
//
// The archive is written by internal/backup, the same call the command makes.

// StateArchive is one server-state backup on disk.
type StateArchive struct {
	Name  string    `json:"name"`
	Size  int64     `json:"size"`
	Taken time.Time `json:"taken"`
}

// ServerStateResponse is what the card shows.
type ServerStateResponse struct {
	Archives []StateArchive `json:"archives"`
	// Directory is where they are, so an operator can find them over SFTP.
	Directory string `json:"directory"`
	Error     string `json:"error,omitempty"`
}

// ServerStateCreated is one archive that has just been written.
type ServerStateCreated struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Name  string `json:"name,omitempty"`
	Size  int64  `json:"size,omitempty"`
	Files int    `json:"files,omitempty"`
}

// writeState is a seam: the real one reaches the filesystem and every
// credential servlo holds.
var writeState = backup.WriteState

// handleServerState answers GET /api/backup/state with what exists and POST
// with one more.
func handleServerState(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		dir := config.SiteBackupsDir()
		entries, err := backup.List(dir, backup.StateName)
		if err != nil {
			writeJSON(w, ServerStateResponse{Error: err.Error(), Directory: dir})
			return
		}
		out := make([]StateArchive, 0, len(entries))
		for _, e := range entries {
			out = append(out, StateArchive{Name: e.Name, Size: e.Size, Taken: e.Taken})
		}
		writeJSON(w, ServerStateResponse{Archives: out, Directory: dir})
	case http.MethodPost:
		key, err := backup.Key()
		if err != nil {
			writeJSON(w, ServerStateCreated{Error: err.Error()})
			return
		}
		path, man, size, err := writeState(config.SiteBackupsDir(), key, backup.StateOptions{Version: version.Version})
		if err != nil {
			writeJSON(w, ServerStateCreated{Error: err.Error()})
			return
		}
		writeJSON(w, ServerStateCreated{OK: true, Name: baseName(path), Size: size, Files: man.Files})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
