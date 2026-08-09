package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/sftpaccess"
)

const sftpTestPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB1ZQ9K1nQyR9m5D3VvJ8xO1XkS8pR3wHqM2vN0aB4cD alice@laptop\n"

func sftpCLIEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	dir := t.TempDir()
	if err := config.AddSite(config.Site{Name: "acme", Path: dir, Domains: []string{"acme.com"}}); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestSFTPAddAuthorisesAKeyFromAPubFile(t *testing.T) {
	home := sftpCLIEnv(t)
	pub := filepath.Join(t.TempDir(), "id_ed25519.pub")
	if err := os.WriteFile(pub, []byte(sftpTestPublicKey), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := sftpAdd("acme.com", "alice-laptop", pub); err != nil {
		t.Fatal(err)
	}

	keys, err := sftpaccess.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Site != "acme.com" {
		t.Fatalf("keys = %+v", keys)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "authorized_keys")); err != nil {
		t.Fatalf("authorized_keys: %v", err)
	}
}

// Pasting a private key into a panel or a terminal is a mistake somebody makes
// once, and it should not be the mistake that puts it on disk.
func TestSFTPAddRefusesAPrivateKey(t *testing.T) {
	sftpCLIEnv(t)
	priv := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(priv, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := sftpAdd("acme.com", "alice-laptop", priv)

	if err == nil || !strings.Contains(err.Error(), "private key") {
		t.Fatalf("err = %v, want a refusal naming the private key", err)
	}
}

// Nothing privileged is executed. The setup command stages a file servlo can
// write and prints what a human has to run.
func TestSFTPSetupStagesAFileAndRunsNothing(t *testing.T) {
	home := sftpCLIEnv(t)
	pub := filepath.Join(t.TempDir(), "id_ed25519.pub")
	if err := os.WriteFile(pub, []byte(sftpTestPublicKey), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sftpAdd("acme.com", "alice-laptop", pub); err != nil {
		t.Fatal(err)
	}

	if err := sftpSetup(); err != nil {
		t.Fatal(err)
	}

	staged := filepath.Join(home, ".config", "servlo", "sftp", filepath.Base(sftpaccess.SSHDDropIn))
	body, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("nothing was staged: %v", err)
	}
	if !strings.Contains(string(body), "ChrootDirectory "+sftpaccess.ChrootBase+"/acme.com") {
		t.Fatalf("staged config = %s", body)
	}
	// The one file under /etc this feature would ever touch is untouched,
	// because servlo is not root and does not ask to be.
	if _, err := os.Stat(sftpaccess.SSHDDropIn); err == nil {
		t.Fatal("servlo wrote into /etc/ssh")
	}
}

func TestSFTPConfinementSaysSoWhenNothingIsInstalled(t *testing.T) {
	sftpCLIEnv(t)

	confined, detail := sftpConfinement()

	if confined {
		t.Fatal("confinement is reported with no sshd block installed")
	}
	if !strings.Contains(detail, "servlo sftp setup") {
		t.Fatalf("detail = %q, want it to say what to do", detail)
	}
}
