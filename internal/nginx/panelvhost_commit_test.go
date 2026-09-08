package nginx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// §3.4 admits no exception, and these two were the exception.
//
// nginx loads its whole configuration or none of it, so one file it refuses
// takes down every site on the machine rather than only the one whose file it
// is. That is why every generated site vhost is written through commitVhost:
// back up what is there, write, ask nginx, put the old file back if nginx
// objects. commitvhost.go says so in its own header, and says the rule covers
// config servlo wrote itself.
//
// The panel's two vhosts were written with a bare os.WriteFile. No validation,
// no backup, nothing to roll back to.
func TestPanelVhostIsValidatedAndBackedUp(t *testing.T) {
	dir := isolateNginx(t)

	var asked int
	restore := swapVhostTest(func() (string, error) { asked++; return "ok", nil })
	defer restore()

	if err := EnsurePanelVhost("panel.example.com", false); err != nil {
		t.Fatal(err)
	}
	if asked == 0 {
		t.Error("the panel vhost was written without asking nginx whether it loads")
	}

	// A second, different write has something to back up.
	asked = 0
	if err := EnsurePanelVhost("panel.example.net", false); err != nil {
		t.Fatal(err)
	}
	backups, err := os.ReadDir(config.NginxConfDBkp())
	if err != nil {
		t.Fatalf("no backup directory after replacing the panel vhost: %v", err)
	}
	if len(backups) == 0 {
		t.Error("replacing the panel vhost kept no backup of what it replaced")
	}
	_ = dir
}

// A vhost nginx refuses must not be left on disk. The whole point of asking is
// that the previous file goes back.
func TestPanelVhostRollsBackWhenNginxRefusesIt(t *testing.T) {
	isolateNginx(t)

	restore := swapVhostTest(func() (string, error) { return "ok", nil })
	if err := EnsurePanelVhost("panel.example.com", false); err != nil {
		t.Fatal(err)
	}
	restore()

	good, err := os.ReadFile(PanelVhostPath())
	if err != nil {
		t.Fatal(err)
	}

	restore = swapVhostTest(func() (string, error) {
		return "nginx: [emerg] bad thing in " + PanelVhostPath(), errRefused
	})
	defer restore()

	if err := EnsurePanelVhost("panel.example.net", false); err == nil {
		t.Error("a panel vhost nginx refused was reported as written")
	}
	after, err := os.ReadFile(PanelVhostPath())
	if err != nil {
		t.Fatalf("the panel vhost is gone after a refused write: %v", err)
	}
	if string(after) != string(good) {
		t.Error("a refused panel vhost was left on disk instead of the one that worked")
	}
}

// The repair scan matches a vhost file against the sites by its filename, so
// servlo-panel.conf can never match one: there is no site called servlo-panel.
// Falling through to the orphan branch deletes the operator's panel vhost.
//
// It is the panel domain's own state to decide, and ApplyPanelDomain decides it
// on every start from whether a certificate exists, which is the same question
// the repair asks. So this file belongs with _default.conf and
// servlo.localhost.conf: not the repair's to touch.
func TestRepairVhostsLeavesThePanelVhostAlone(t *testing.T) {
	isolateNginx(t)

	restore := swapVhostTest(func() (string, error) { return "ok", nil })
	defer restore()

	// A secured panel vhost whose certificate is not there.
	if err := EnsurePanelVhost("panel.example.com", true); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(PanelVhostPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "ssl_certificate") {
		t.Skip("the secured panel vhost carries no certificate line to lose")
	}

	RepairVhosts()

	if _, err := os.Stat(PanelVhostPath()); err != nil {
		t.Error("the repair scan deleted the panel's own vhost as an orphan")
	}
}

func isolateNginx(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "servlo")
}

func swapVhostTest(fn func() (string, error)) func() {
	old := vhostTestFn
	vhostTestFn = fn
	return func() { vhostTestFn = old }
}

var errRefused = errors.New("nginx refused it")
