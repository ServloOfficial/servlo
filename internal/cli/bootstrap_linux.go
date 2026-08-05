//go:build linux

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/realrashid/servlo/internal/certs"
	"github.com/realrashid/servlo/internal/feedback"
)

// runBootstrapSystem performs the root prerequisites the per-user install relies
// on: the unprivileged-port sysctl and systemd linger. It runs as root already,
// called by a package maintainer script or by an interactive install
// re-executing itself under sudo, so unlike the install's own port step it
// applies rather than prints.
func runBootstrapSystem(target string) error {
	feedback.Header("Bootstrapping system for servlo")

	if err := writePortDropIn(unprivPortDropIn, bootstrapRunner); err != nil {
		feedback.Warn("enabling unprivileged ports: %v", err)
	} else {
		feedback.Done("unprivileged ports enabled for 80/443")
	}

	if target == "" {
		return nil
	}
	if err := enableLinger(target, bootstrapRunner); err != nil {
		feedback.Warn("enabling linger for %s: %v", target, err)
	} else {
		feedback.Done("systemd linger enabled for " + target)
	}
	return nil
}

// runBootstrapUntrustCA drops servlo's CA from the system trust store. Pairs with
// runBootstrapTrustCA on uninstall, since mkcert's own -uninstall only knows the
// anchor filename mkcert itself wrote.
func runBootstrapUntrustCA() error {
	if err := certs.UntrustCAFromSystemStore(); err != nil {
		if errors.Is(err, certs.ErrNoSystemTrustStore) {
			return nil
		}
		return fmt.Errorf("removing CA from system store: %w", err)
	}
	feedback.Done("mkcert CA removed from the system store")
	return nil
}

// runBootstrapTrustCA installs the user-generated mkcert root CA into the system
// trust store, the one managed-DNS step the per-user install cannot do without
// an interactive sudo. Run after the per-user install has generated the CA. A
// missing CA is not an error (localhost-mode installs have none). caRoot names
// the CAROOT directly; without it the target user's default location is used,
// since asking mkcert as root would resolve root's CAROOT instead.
func runBootstrapTrustCA(target, caRoot string) error {
	if target == "" && caRoot == "" {
		return fmt.Errorf("--trust-ca needs a target user (pass --user)")
	}
	if caRoot == "" {
		u, err := user.Lookup(target)
		if err != nil {
			return fmt.Errorf("looking up user %s: %w", target, err)
		}
		caRoot = filepath.Join(u.HomeDir, ".local", "share", "mkcert")
	}
	caPath := filepath.Join(caRoot, "rootCA.pem")
	pem, err := os.ReadFile(caPath)
	if err != nil {
		feedback.Note("no mkcert CA at " + caPath + " yet, skipping system trust")
		return nil
	}
	if err := certs.TrustCAInSystemStore(pem); err != nil {
		if errors.Is(err, certs.ErrNoSystemTrustStore) {
			feedback.Note("no system CA trust store on this distro; trust " + caPath + " through your system configuration (NixOS: security.pki.certificateFiles)")
			return nil
		}
		return fmt.Errorf("trusting CA in system store: %w", err)
	}
	feedback.Done("mkcert CA trusted in the system store")
	return nil
}
