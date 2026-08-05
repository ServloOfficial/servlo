//go:build linux

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/realrashid/servlo/internal/certs"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/feedback"
)

func downloadBinaries(w io.Writer) error {
	binDir := config.BinDir()
	var pins pinnedTools

	// composer
	composerPharPath := filepath.Join(binDir, "composer.phar")
	if _, err := os.Stat(composerPharPath); os.IsNotExist(err) {
		if err := replaceTool(&pins, "composer", composerPharPath, w); err != nil {
			return fmt.Errorf("composer download: %w", err)
		}
	}

	// fnm — skipped when the user drives Node via their own nvm, since servlo never
	// provisions nvm and fnm would sit unused.
	// Switching back with `servlo node:manager fnm` calls ensureFnmBinary on demand.
	cfg, _ := config.LoadGlobal()
	if cfg == nil || cfg.NodeManager() != "nvm" {
		if err := ensureFnmBinary(w); err != nil {
			return err
		}
	}

	// mkcert
	mkcertPath := certs.MkcertPath()
	if _, err := os.Stat(mkcertPath); os.IsNotExist(err) {
		if err := replaceTool(&pins, "mkcert", mkcertPath, w); err != nil {
			return fmt.Errorf("mkcert download: %w", err)
		}
	}

	return nil
}

// ensurePortForwarding records the port strategy and prints whatever the
// operator still has to run for it. Named for the call site in install; the
// decision itself lives in internal/ports.
func ensurePortForwarding() error { return applyPortStrategy() }

// Seams for the root pass, so tests can drive each precondition and observe the
// escalation without a real kernel, login manager, or sudo.
var (
	lingerNeeded      = defaultLingerNeeded
	sudoSelfRunner    = defaultSudoSelfRunner
	legacySystemSetup = defaultLegacySystemSetup
)

// defaultSudoSelfRunner re-executes this binary under sudo. The absolute path
// from os.Executable sidesteps sudo's secure_path, which does not carry
// ~/.local/bin.
func defaultSudoSelfRunner(args ...string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command("sudo", append([]string{self}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// systemSetupNeeded reports whether any root-level step still has to run. An
// install where they all already apply must not ask for a password to do
// nothing, which is the common case on every reinstall and update.
func systemSetupNeeded() bool {
	return lingerNeeded()
}

// runSystemSetup applies the machine-global half of the install through a
// single `sudo servlo bootstrap --system`, the same entry point a package
// maintainer script calls, so both install routes share one implementation of
// the sysctl and linger steps and ask for a password once. A host
// where the re-exec cannot run falls back to the individual steps.
func runSystemSetup() error {
	if !systemSetupNeeded() {
		return nil
	}
	feedback.Sudo("Applying system setup")
	if err := sudoSelfRunner("bootstrap", "--system"); err != nil {
		feedback.Warn("system setup through sudo failed (%v), applying the steps individually", err)
		return legacySystemSetup()
	}
	return nil
}

// defaultLegacySystemSetup is the pre-bootstrap path, kept as the fallback for
// hosts without a usable sudo. Each step prompts on its own.
func defaultLegacySystemSetup() error {
	if err := ensureSystemdLinger(); err != nil {
		feedback.Warn("%v", err)
	}
	return nil
}

// ensureMkcertCA generates the root CA and gets it trusted. Generation and the
// browser NSS store are per-user and need no root; the system trust store is
// done by re-executing `servlo bootstrap --trust-ca`, so the CA reaches the same
// place by the same code on both install routes. An unattended run stops after
// generation because its maintainer script runs the trust pass itself.
func ensureMkcertCA(unattended bool) {
	gen := exec.Command(certs.MkcertPath(), "-install")
	gen.Env = append(os.Environ(), "TRUST_STORES=nss")
	gen.Stdout, gen.Stderr = io.Discard, io.Discard
	gen.Run() //nolint:errcheck

	if unattended || certs.CATrusted() {
		return
	}
	args := []string{"bootstrap", "--trust-ca"}
	if root, err := certs.CARoot(); err == nil && root != "" {
		args = append(args, "--ca-root", root)
	}
	feedback.Sudo("Trusting the mkcert CA in the system store")
	if err := sudoSelfRunner(args...); err != nil {
		feedback.Warn("trusting the mkcert CA: %v", err)
	}
}

// removeSystemTrustAnchor drops the CA that ensureMkcertCA installed. mkcert's
// own -uninstall keys on the filename it wrote, so it never removes this one.
func removeSystemTrustAnchor() {
	if !certs.SystemTrustAnchorPresent() {
		return
	}
	feedback.Sudo("Removing the mkcert CA from the system trust store")
	if err := sudoSelfRunner("bootstrap", "--untrust-ca"); err != nil {
		feedback.Warn("removing the mkcert CA: %v", err)
	}
}
