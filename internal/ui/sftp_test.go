package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/sftpaccess"
)

const sftpTestKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB1ZQ9K1nQyR9m5D3VvJ8xO1XkS8pR3wHqM2vN0aB4cD alice@laptop"

// sftpEnv gives the handlers a home of their own, so a test never reads or
// writes the authorized_keys of whoever is running it.
func sftpEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	dir := t.TempDir()
	if err := config.AddSite(config.Site{Name: "acme", Path: dir, Domains: []string{"acme.com"}}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHandleSFTP_AuthorisesAKeyAndListsIt(t *testing.T) {
	sftpEnv(t)

	body, _ := json.Marshal(sftpKeyRequest{Domain: "acme.com", Label: "alice-laptop", Key: sftpTestKey})
	rec := httptest.NewRecorder()
	handleSFTP(rec, httptest.NewRequest(http.MethodPost, "/api/sftp", bytes.NewReader(body)))
	if res := decodeFiles[filesResponse](t, rec); !res.OK {
		t.Fatalf("add = %+v", res)
	}

	rec = httptest.NewRecorder()
	handleSFTP(rec, httptest.NewRequest(http.MethodGet, "/api/sftp", nil))
	status := decodeFiles[SFTPStatus](t, rec)
	if len(status.Sites) != 1 || len(status.Sites[0].Keys) != 1 {
		t.Fatalf("status = %+v", status)
	}
	site := status.Sites[0]
	if site.Domain != "acme.com" || site.Port < 2200 {
		t.Fatalf("site = %+v", site)
	}
	// Nothing is confined until the operator has run the block, and the panel
	// has to say so rather than implying a lock that is not there.
	if site.Confined {
		t.Fatal("a site is reported confined with no sshd block installed")
	}
	if status.User == "" {
		t.Fatal("the status does not name the account sessions log in as")
	}
}

// The sudo block is printed, with sudo, and servlo runs none of it.
func TestHandleSFTP_PrintsTheSudoBlockRatherThanRunningIt(t *testing.T) {
	sftpEnv(t)
	body, _ := json.Marshal(sftpKeyRequest{Domain: "acme.com", Label: "alice-laptop", Key: sftpTestKey})
	handleSFTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/sftp", bytes.NewReader(body)))

	rec := httptest.NewRecorder()
	handleSFTP(rec, httptest.NewRequest(http.MethodGet, "/api/sftp", nil))
	status := decodeFiles[SFTPStatus](t, rec)

	if len(status.Commands) == 0 {
		t.Fatal("no commands were printed")
	}
	for _, command := range status.Commands {
		if !strings.HasPrefix(command, "sudo ") {
			t.Fatalf("a printed command is missing sudo: %q", command)
		}
	}
	if !strings.Contains(strings.Join(status.Commands, "\n"), sftpaccess.SSHDDropIn) {
		t.Fatalf("the block does not install the drop-in:\n%s", strings.Join(status.Commands, "\n"))
	}
	// The staged file is real, so an operator can read it before installing it.
	if _, err := os.Stat(status.StagedPath); err != nil {
		t.Fatalf("staged config: %v", err)
	}
	staged, err := os.ReadFile(status.StagedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(staged), "ChrootDirectory") {
		t.Fatalf("staged config does not chroot anything:\n%s", staged)
	}
	// Servlo never disables password login, and the file it asks an operator to
	// install is the one place it could.
	if strings.Contains(string(staged), "\nPasswordAuthentication") {
		t.Fatalf("the staged config touches password authentication:\n%s", staged)
	}
}

func TestHandleSFTP_RefusesAKeyForASiteThatDoesNotExist(t *testing.T) {
	sftpEnv(t)

	body, _ := json.Marshal(sftpKeyRequest{Domain: "nope.com", Label: "alice-laptop", Key: sftpTestKey})
	rec := httptest.NewRecorder()
	handleSFTP(rec, httptest.NewRequest(http.MethodPost, "/api/sftp", bytes.NewReader(body)))

	if res := decodeFiles[filesResponse](t, rec); res.OK {
		t.Fatal("a key was authorised for a site that is not here")
	}
}

func TestHandleSFTPKey_WithdrawsByFingerprint(t *testing.T) {
	sftpEnv(t)
	body, _ := json.Marshal(sftpKeyRequest{Domain: "acme.com", Label: "alice-laptop", Key: sftpTestKey})
	handleSFTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/sftp", bytes.NewReader(body)))
	keys, err := sftpaccess.List()
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys = %+v, %v", keys, err)
	}

	rec := httptest.NewRecorder()
	handleSFTPKey(rec, httptest.NewRequest(http.MethodDelete, "/api/sftp/keys/"+keys[0].Fingerprint, nil))

	if res := decodeFiles[filesResponse](t, rec); !res.OK {
		t.Fatalf("delete = %+v", res)
	}
	after, err := sftpaccess.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("keys = %+v, want none", after)
	}
}

func TestSFTPRoutes_RefuseTheWrongMethod(t *testing.T) {
	sftpEnv(t)

	rec := httptest.NewRecorder()
	handleSFTP(rec, httptest.NewRequest(http.MethodPut, "/api/sftp", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /api/sftp = %d, want 405", rec.Code)
	}
	rec = httptest.NewRecorder()
	handleSFTPKey(rec, httptest.NewRequest(http.MethodPost, "/api/sftp/keys/SHA256:abc", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/sftp/keys/... = %d, want 405", rec.Code)
	}
}
