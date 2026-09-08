package ui

import (
	"net/http"
	"time"

	"github.com/ServloOfficial/servlo/internal/backup"
	"github.com/ServloOfficial/servlo/internal/config"
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
	// SendErrors is one entry per destination the archive could not be copied
	// to. The archive is on this server either way, so these travel beside a
	// successful write rather than instead of one.
	SendErrors []string `json:"send_errors,omitempty"`
}

// backUpState is a seam: the real one reaches the filesystem, every credential
// servlo holds, and whatever destinations the operator has configured.
//
// It is the shared runner rather than the bare archive writer, so the button
// and `servlo backup state` do the same thing. They did not: both wrote an
// archive to local disk and neither sent it anywhere, which made the panel's
// Server state card a backup of the machine, kept on the machine.
var backUpState = func(key []byte, opts backup.StateOptions) (backup.Record, error) {
	return backup.ForState().Run(key, opts)
}

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
		rec, err := backUpState(key, backup.StateOptions{})
		if err != nil {
			writeJSON(w, ServerStateCreated{Error: err.Error()})
			return
		}
		// A destination that could not be reached is said here rather than
		// swallowed: the card's whole claim is that this archive is somewhere
		// other than this machine.
		var sendErrors []string
		for _, sendErr := range rec.SendErrors {
			sendErrors = append(sendErrors, sendErr.Error())
		}
		writeJSON(w, ServerStateCreated{
			OK: true, Name: baseName(rec.Path), Size: rec.Size,
			Files: rec.Manifest.Files, SendErrors: sendErrors,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
