// Package distro answers one question: is this host a platform Servlo will run
// on? Servlo targets Ubuntu 24.04 LTS and nothing else, so detection is a gate
// rather than a dispatch table. There is deliberately no IsDebian/IsFedora/
// IsArch here: a per-distro branch is the shape that grows back into
// multi-distro support, which PRD §3 rules out.
package distro

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The oldest Ubuntu Servlo supports. 22.04 ships podman 3.4.4, below the 4.5
// minimum the quadlet units need, and that release cannot provide a newer one,
// so the refusal points at the release upgrade rather than at a package.
const (
	MinUbuntuMajor = 24
	MinUbuntuMinor = 4
)

// Distro holds what /etc/os-release says about this host.
type Distro struct {
	ID         string
	IDLike     string
	VersionID  string
	PrettyName string
}

// Detect parses /etc/os-release.
func Detect() (*Distro, error) {
	return detectFromPath("/etc/os-release")
}

func detectFromPath(path string) (*Distro, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	d := &Distro{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch strings.TrimSpace(key) {
		case "ID":
			d.ID = val
		case "ID_LIKE":
			d.IDLike = val
		case "VERSION_ID":
			d.VersionID = val
		case "PRETTY_NAME":
			d.PrettyName = val
		}
	}
	return d, scanner.Err()
}

// IsUbuntu reports whether this is Ubuntu itself. ID_LIKE is deliberately not
// consulted: Mint, Pop!_OS and the rest declare ID_LIKE=ubuntu but are not what
// Servlo is tested on, and a half-install on an untested base is worse than a
// clear refusal.
func (d *Distro) IsUbuntu() bool { return d.ID == "ubuntu" }

// Supported returns nil when Servlo will run here, and otherwise an error that
// names both what was found and the way forward. Callers surface the message
// as-is; it is written to be read by an operator, not parsed.
func (d *Distro) Supported() error {
	if !d.IsUbuntu() {
		return fmt.Errorf("Servlo requires Ubuntu %d.%02d LTS; this system is %s",
			MinUbuntuMajor, MinUbuntuMinor, d.describe())
	}
	major, minor, ok := d.version()
	if !ok {
		return fmt.Errorf("Servlo requires Ubuntu %d.%02d LTS; this system reports no usable VERSION_ID (%s)",
			MinUbuntuMajor, MinUbuntuMinor, d.describe())
	}
	if major < MinUbuntuMajor || (major == MinUbuntuMajor && minor < MinUbuntuMinor) {
		return fmt.Errorf("Servlo requires Ubuntu %d.%02d LTS or newer; this system is %s. "+
			"That release cannot provide podman 4.5, which the container units need. "+
			"Upgrade first: sudo do-release-upgrade",
			MinUbuntuMajor, MinUbuntuMinor, d.describe())
	}
	return nil
}

// version splits VERSION_ID ("24.04") into its numeric parts.
func (d *Distro) version() (major, minor int, ok bool) {
	majStr, minStr, found := strings.Cut(d.VersionID, ".")
	if !found {
		return 0, 0, false
	}
	major, err := strconv.Atoi(majStr)
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(minStr)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// describe is the best human label available for this host.
func (d *Distro) describe() string {
	if d.PrettyName != "" {
		return d.PrettyName
	}
	if d.ID != "" {
		return d.ID
	}
	return "unrecognised"
}
