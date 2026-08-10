package backupdest

import (
	"os"
	"path/filepath"
	"testing"
)

func withHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// An S3 secret key is in this file, so it is not readable by anyone else on the
// box, the same as the connection registry beside it.
func TestAdd_WritesTheStorePrivate(t *testing.T) {
	withHome(t)
	if err := Add(spaces()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "servlo", storeFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("the destination store is %04o, want 0600: it holds a secret key", perm)
	}
}

// Adding a name that is taken is refused rather than replacing. Replacing
// silently would leave an operator believing archives go to two places when
// they go to one.
func TestAdd_RefusesADuplicateName(t *testing.T) {
	withHome(t)
	if err := Add(spaces()); err != nil {
		t.Fatal(err)
	}
	other := spaces()
	other.Bucket = "somewhere-else"
	if err := Add(other); err == nil {
		t.Fatal("a second destination with the same name replaced the first")
	}
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Destinations) != 1 || reg.Destinations[0].Bucket != "acme-backups" {
		t.Errorf("the original was changed: %+v", reg.Destinations)
	}
}

// A destination that cannot work never reaches the file, so the first scheduled
// upload is not the thing that discovers it.
func TestAdd_RefusesAnInvalidDestination(t *testing.T) {
	withHome(t)
	broken := remote()
	broken.Path = ""
	if err := Add(broken); err == nil {
		t.Fatal("an SFTP destination with no path was stored")
	}
	if reg, _ := Load(); len(reg.Destinations) != 0 {
		t.Errorf("it was stored anyway: %+v", reg.Destinations)
	}
}

// No file is no destinations, not an error. A server with no offsite copy is
// the state every install starts in.
func TestLoad_NoFileIsNoDestinations(t *testing.T) {
	withHome(t)
	reg, err := Load()
	if err != nil {
		t.Fatalf("a server that has configured nothing reported an error: %v", err)
	}
	if len(reg.Destinations) != 0 {
		t.Errorf("got %d destinations from nowhere", len(reg.Destinations))
	}
}

// Removing forgets the destination and leaves what is already there alone.
func TestRemove_ForgetsOneAndKeepsTheRest(t *testing.T) {
	withHome(t)
	if err := Add(spaces()); err != nil {
		t.Fatal(err)
	}
	if err := Add(remote()); err != nil {
		t.Fatal(err)
	}
	if err := Remove("spaces"); err != nil {
		t.Fatal(err)
	}
	reg, _ := Load()
	if len(reg.Destinations) != 1 || reg.Destinations[0].Name != "offsite" {
		t.Errorf("remove left %+v", reg.Destinations)
	}
	if err := Remove("spaces"); err == nil {
		t.Error("removing a destination that is not there reported success")
	}
}
