package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/backup"
	"github.com/ServloOfficial/servlo/internal/config"
)

// The list is what makes the card worth having: an operator who has never
// taken one sees an empty list and a button, and one who has sees whether the
// newest is from today or from March.
func TestHandleServerState_ListsWhatIsActuallyOnDisk(t *testing.T) {
	dir := stateSandbox(t)
	// Two archives and one file that is not one, so the list is proved to be
	// reading the directory rather than echoing whatever is in it.
	writeArchive(t, filepath.Join(dir, backup.StateName+"-20260301-101500"+backup.Extension), 4096)
	writeArchive(t, filepath.Join(dir, backup.StateName+"-20260302-101500"+backup.Extension), 8192)
	writeArchive(t, filepath.Join(dir, "acme-com-20260302-101500"+backup.Extension), 100)
	writeArchive(t, filepath.Join(dir, "notes.txt"), 10)

	var got ServerStateResponse
	getState(t, &got)

	if len(got.Archives) != 2 {
		t.Fatalf("expected the two state archives, got %d: %+v", len(got.Archives), got.Archives)
	}
	// Newest first, or the card's first row is the least useful one.
	if !got.Archives[0].Taken.After(got.Archives[1].Taken) {
		t.Errorf("archives are not newest first: %+v", got.Archives)
	}
	if got.Archives[0].Size == 0 {
		t.Error("an archive reported no size, so the card cannot say how big it is")
	}
	if got.Directory == "" {
		t.Error("the response does not say where the archives are")
	}
}

func TestHandleServerState_EmptyIsAListWithNothingInIt(t *testing.T) {
	stateSandbox(t)
	var got ServerStateResponse
	getState(t, &got)
	if got.Error != "" {
		t.Fatalf("a server that has never taken one reported an error: %s", got.Error)
	}
	if got.Archives == nil {
		t.Error("archives came back null rather than empty, which a client reads as unknown")
	}
}

func TestHandleServerState_CreatesOneAndNamesIt(t *testing.T) {
	stateSandbox(t)
	defer swapWriteState(func(dir string, key []byte, opts backup.StateOptions) (string, backup.Manifest, int64, error) {
		return filepath.Join(dir, "servlo-state-20260309-120000.servlobak"), backup.Manifest{Files: 31}, 20481, nil
	})()

	var got ServerStateCreated
	postState(t, &got)
	if !got.OK || got.Name != "servlo-state-20260309-120000.servlobak" {
		t.Fatalf("the panel has nothing to show for the archive it just took: %+v", got)
	}
	if got.Files != 31 || got.Size == 0 {
		t.Errorf("the response does not say what was written: %+v", got)
	}
}

// A failure has to come back as one. A card that says nothing after a click is
// a card that lets an operator believe they have a backup they do not have.
func TestHandleServerState_ReportsAFailureToWrite(t *testing.T) {
	stateSandbox(t)
	defer swapWriteState(func(string, []byte, backup.StateOptions) (string, backup.Manifest, int64, error) {
		return "", backup.Manifest{}, 0, errors.New("no space left on device")
	})()

	var got ServerStateCreated
	postState(t, &got)
	if got.OK || !strings.Contains(got.Error, "no space left") {
		t.Fatalf("a failed backup did not come back as one: %+v", got)
	}
}

func TestHandleServerState_RefusesOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	handleServerState(rec, httptest.NewRequest(http.MethodDelete, "/api/backup/state", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE was answered with %d", rec.Code)
	}
}

func stateSandbox(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	dir := config.SiteBackupsDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeArchive(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0600); err != nil {
		t.Fatal(err)
	}
}

func swapWriteState(fn func(string, []byte, backup.StateOptions) (string, backup.Manifest, int64, error)) func() {
	old := writeState
	writeState = fn
	return func() { writeState = old }
}

func getState(t *testing.T, into any) {
	t.Helper()
	rec := httptest.NewRecorder()
	handleServerState(rec, httptest.NewRequest(http.MethodGet, "/api/backup/state", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("reading the response: %v\n%s", err, rec.Body)
	}
}

func postState(t *testing.T, into any) {
	t.Helper()
	rec := httptest.NewRecorder()
	handleServerState(rec, httptest.NewRequest(http.MethodPost, "/api/backup/state", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("reading the response: %v\n%s", err, rec.Body)
	}
}
