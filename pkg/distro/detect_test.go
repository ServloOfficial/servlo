package distro

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOsRelease(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDetect_Ubuntu2404(t *testing.T) {
	f := writeOsRelease(t, `ID=ubuntu
ID_LIKE=debian
VERSION_ID="24.04"
PRETTY_NAME="Ubuntu 24.04.1 LTS"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatalf("detectFromPath: %v", err)
	}
	if !d.IsUbuntu() {
		t.Errorf("expected IsUbuntu() for ID=ubuntu, got %q", d.ID)
	}
	if d.VersionID != "24.04" {
		t.Errorf("VersionID = %q, want 24.04", d.VersionID)
	}
	if err := d.Supported(); err != nil {
		t.Errorf("Ubuntu 24.04 must be supported, got: %v", err)
	}
}

// A Debian derivative declaring ID_LIKE=debian is not Ubuntu. Accepting one
// would put Servlo on an untested base, which is the half-install this gate
// exists to prevent.
func TestDetect_DebianDerivativeIsNotUbuntu(t *testing.T) {
	for _, id := range []string{"debian", "linuxmint", "pop", "elementary"} {
		f := writeOsRelease(t, "ID="+id+"\nID_LIKE=\"ubuntu debian\"\nPRETTY_NAME=\"Some Debian thing\"\n")
		d, err := detectFromPath(f)
		if err != nil {
			t.Fatalf("detectFromPath(%s): %v", id, err)
		}
		if d.IsUbuntu() {
			t.Errorf("%s must not report as Ubuntu", id)
		}
		if err := d.Supported(); err == nil {
			t.Errorf("%s must not be supported", id)
		}
	}
}

// The refusal has to name what it found, or the operator has to go and look.
func TestSupported_RefusalNamesTheDistro(t *testing.T) {
	f := writeOsRelease(t, `ID=fedora
PRETTY_NAME="Fedora Linux 41 (Server Edition)"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatalf("detectFromPath: %v", err)
	}
	err = d.Supported()
	if err == nil {
		t.Fatal("Fedora must not be supported")
	}
	if got := err.Error(); !strings.Contains(got, "Fedora Linux 41") || !strings.Contains(got, "24.04") {
		t.Errorf("refusal should name what was found and what is required, got: %s", got)
	}
}

// 22.04 is refused because it ships podman 3.4.4, below the quadlet minimum.
// The message has to carry the way out, not just the verdict.
func TestSupported_OlderUbuntuNamesTheUpgradePath(t *testing.T) {
	f := writeOsRelease(t, `ID=ubuntu
VERSION_ID="22.04"
PRETTY_NAME="Ubuntu 22.04.5 LTS"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatalf("detectFromPath: %v", err)
	}
	err = d.Supported()
	if err == nil {
		t.Fatal("Ubuntu 22.04 must not be supported")
	}
	if got := err.Error(); !strings.Contains(got, "do-release-upgrade") {
		t.Errorf("refusal should name the upgrade path, got: %s", got)
	}
}

// The minimum is a floor, not an exact match, so a newer Ubuntu is accepted.
func TestSupported_NewerUbuntuIsAccepted(t *testing.T) {
	for _, v := range []string{"24.10", "25.04", "26.04"} {
		f := writeOsRelease(t, "ID=ubuntu\nVERSION_ID=\""+v+"\"\nPRETTY_NAME=\"Ubuntu "+v+"\"\n")
		d, err := detectFromPath(f)
		if err != nil {
			t.Fatalf("detectFromPath(%s): %v", v, err)
		}
		if err := d.Supported(); err != nil {
			t.Errorf("Ubuntu %s should be accepted, got: %v", v, err)
		}
	}
}

func TestDetect_MissingFileIsAnError(t *testing.T) {
	if _, err := detectFromPath(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing os-release must be an error, not a silent pass")
	}
}
