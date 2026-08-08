package nginx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func vhostHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	dir := config.NginxConfD()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// stubVhostTest replaces `nginx -t` with a scripted verdict and counts the
// calls, so these tests are about the commit path rather than a container.
func stubVhostTest(t *testing.T, out string, err error) *int {
	t.Helper()
	calls := 0
	prevTest, prevReload := vhostTestFn, vhostReloadFn
	vhostTestFn = func() (string, error) { calls++; return out, err }
	vhostReloadFn = func() error { return nil }
	t.Cleanup(func() { vhostTestFn, vhostReloadFn = prevTest, prevReload })
	return &calls
}

// A generated vhost is nginx config like any other, and a broken one takes down
// every site on the machine, not only this one. §3.4 admits no exception for
// config servlo wrote itself.
func TestCommitVhost_ValidatesBeforeTheWriteStands(t *testing.T) {
	dir := vhostHome(t)
	path := filepath.Join(dir, "shop.example.conf")
	if err := os.WriteFile(path, []byte("server { # the good one\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubVhostTest(t, "nginx: [emerg] unknown directive in "+path+":3", errors.New("exit 1"))

	err := commitVhost(path, []byte("server { oops;\n}\n"))
	if err == nil {
		t.Fatal("a vhost nginx refuses was committed")
	}

	body, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(body), "the good one") {
		t.Errorf("the previous config was not restored:\n%s", body)
	}
	// The operator has to be told what nginx objected to, or the only signal is
	// a site that silently kept its old config.
	if !strings.Contains(err.Error(), "unknown directive") {
		t.Errorf("error = %q, does not carry nginx's own diagnostic", err)
	}
}

// A failure that names some other file is somebody else's broken config, and
// rolling this site back would not fix it while losing a change that was fine.
func TestCommitVhost_KeepsTheWriteWhenTheFailureIsNotThisFile(t *testing.T) {
	dir := vhostHome(t)
	path := filepath.Join(dir, "shop.example.conf")
	stubVhostTest(t, "nginx: [emerg] unknown directive in /etc/nginx/conf.d/other.conf:3", errors.New("exit 1"))

	if err := commitVhost(path, []byte("server { # new\n}\n")); err != nil {
		t.Fatalf("a write was rolled back for another file's error: %v", err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "# new") {
		t.Errorf("the new config was rolled back anyway:\n%s", body)
	}
}

// The same applies to a container that is not running, which is the ordinary
// state during install and the first link: nginx cannot be asked, and refusing
// to write the vhost would mean the site never gets one.
func TestCommitVhost_WritesWhenNginxCannotBeAsked(t *testing.T) {
	dir := vhostHome(t)
	path := filepath.Join(dir, "shop.example.conf")
	stubVhostTest(t, "", errors.New("no such container: servlo-nginx"))

	if err := commitVhost(path, []byte("server {\n}\n")); err != nil {
		t.Fatalf("the vhost was not written while nginx was down: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("no vhost on disk: %v", err)
	}
}

// Every link, every install and every quadlet rewrite regenerates every vhost.
// Backing up and revalidating a file whose bytes did not change would fill the
// disk with identical backups and spend a container round trip per site for
// nothing.
func TestCommitVhost_DoesNothingWhenTheContentIsUnchanged(t *testing.T) {
	dir := vhostHome(t)
	path := filepath.Join(dir, "shop.example.conf")
	content := []byte("server {\n}\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	calls := stubVhostTest(t, "", nil)

	if err := commitVhost(path, content); err != nil {
		t.Fatal(err)
	}

	if *calls != 0 {
		t.Errorf("nginx -t ran %d times for an unchanged file", *calls)
	}
	if backups, _ := ListVhostBackups("shop.example"); len(backups) != 0 {
		t.Errorf("an unchanged write produced %d backups", len(backups))
	}
}

// The backup is what makes the rollback recoverable by hand when the automatic
// one cannot help, and what the restore reads.
func TestCommitVhost_KeepsATimestampedBackupOfWhatItReplaced(t *testing.T) {
	dir := vhostHome(t)
	path := filepath.Join(dir, "shop.example.conf")
	if err := os.WriteFile(path, []byte("server { # first\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubVhostTest(t, "", nil)

	if err := commitVhost(path, []byte("server { # second\n}\n")); err != nil {
		t.Fatal(err)
	}

	backups, err := ListVhostBackups("shop.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("got %d backups, want the one it replaced", len(backups))
	}
	body, err := ReadVhostBackup("shop.example", backups[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# first") {
		t.Errorf("the backup is not what was replaced:\n%s", body)
	}
}

// Backups live outside conf.d. nginx includes conf.d/*.conf, so a backup kept
// beside the live file would be loaded as a second server block for the same
// domain.
func TestCommitVhost_KeepsBackupsOutOfTheDirectoryNginxIncludes(t *testing.T) {
	dir := vhostHome(t)
	path := filepath.Join(dir, "shop.example.conf")
	if err := os.WriteFile(path, []byte("server { # first\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubVhostTest(t, "", nil)

	if err := commitVhost(path, []byte("server { # second\n}\n")); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "shop.example.conf" {
			t.Errorf("%q sits in the directory nginx includes", e.Name())
		}
	}
}

// One click, per the story: the operator picks a backup and the site goes back
// to it, through the same validation as any other write.
func TestRestoreVhost_PutsABackupBackOverTheLiveConfig(t *testing.T) {
	dir := vhostHome(t)
	path := filepath.Join(dir, "shop.example.conf")
	if err := os.WriteFile(path, []byte("server { # first\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubVhostTest(t, "", nil)
	if err := commitVhost(path, []byte("server { # second\n}\n")); err != nil {
		t.Fatal(err)
	}

	backups, _ := ListVhostBackups("shop.example")
	if err := RestoreVhost("shop.example", backups[0].Name); err != nil {
		t.Fatalf("RestoreVhost: %v", err)
	}

	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "# first") {
		t.Errorf("the live config is not the restored backup:\n%s", body)
	}
}

// A backup name reaches this from the panel and becomes a path.
func TestRestoreVhost_RefusesANameThatIsAPath(t *testing.T) {
	vhostHome(t)
	stubVhostTest(t, "", nil)

	for _, bad := range []string{"../../../etc/passwd", "a/b", "", "shop.example.conf"} {
		err := RestoreVhost("shop.example", bad)
		if err == nil {
			t.Errorf("backup name %q was accepted", bad)
			continue
		}
		// Named rather than a bare not-exist. The reader had a list of backups
		// in front of them, so "no such file" reads as servlo losing one.
		if !strings.Contains(err.Error(), "not a backup") {
			t.Errorf("backup name %q was refused with %q, which does not say why", bad, err)
		}
	}
}

// The domain is the other half of the same path.
func TestVhostBackups_RefuseADomainThatIsAPath(t *testing.T) {
	vhostHome(t)

	for _, bad := range []string{"../evil", "a/b", ""} {
		if _, err := ListVhostBackups(bad); err == nil {
			t.Errorf("domain %q was accepted", bad)
		}
	}
}
