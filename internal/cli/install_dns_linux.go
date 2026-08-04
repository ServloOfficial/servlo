//go:build linux

package cli

import (
	"io"
	"strings"

	"github.com/realrashid/servlo/internal/dns"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/services"
)

// writeDNSUnit writes the container quadlet for the dnsmasq DNS service on Linux.
func writeDNSUnit(_ io.Writer) error {
	content, err := podman.GetQuadletTemplate("servlo-dns.container")
	if err != nil {
		return err
	}
	return services.Mgr.WriteContainerUnit("servlo-dns", content)
}

// ensureDNSImageForStart ensures the servlo-dnsmasq container image exists on Linux.
func ensureDNSImageForStart() {
	// Build the dnsmasq image if it doesn't exist. Ignore errors — the image
	// will be pulled/built during RunParallel if missing.
	containerfile := "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache dnsmasq\n"
	if !podman.ImageExists("servlo-dnsmasq:local") {
		cmd := podman.Cmd("build", "-t", "servlo-dnsmasq:local", "-")
		cmd.Stdin = strings.NewReader(containerfile)
		cmd.Run() //nolint:errcheck
	}
}

// pullDNSImages returns build jobs to pull alpine and build the dnsmasq container image.
func pullDNSImages() []BuildJob {
	return []BuildJob{
		{
			Label: "Pulling alpine:latest",
			Run: func(w io.Writer) error {
				cmd := podman.Cmd("pull", "docker.io/library/alpine:latest")
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		},
		{
			Label: "Building dnsmasq image",
			Run: func(w io.Writer) error {
				containerfile := "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache dnsmasq\n"
				cmd := podman.Cmd("build", "-t", "servlo-dnsmasq:local", "-")
				cmd.Stdin = strings.NewReader(containerfile)
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		},
	}
}

// isDNSContainerUnit returns true on Linux since DNS uses a Podman container.
func isDNSContainerUnit() bool { return true }

// ensureDNSServiceUpdated is a no-op on Linux — DNS always uses a container.
func ensureDNSServiceUpdated(_ io.Writer) error { return nil }

// removeDNSContainerIfRunning is a no-op on Linux.
func removeDNSContainerIfRunning() {}

// nativeDNSRestart is a no-op on Linux — DNS is a container unit managed by systemd.
func nativeDNSRestart() error { return nil }

// needsDNSServiceInstall always returns false on Linux (container quadlet handles it).
func needsDNSServiceInstall() bool { return false }

// teardownDNS stops the servlo-dns container, removes its quadlet, and reloads
// the user manager so a subsequent `servlo install` does not silently restart
// the unit. Called from runInstall when the user flips dns.enabled from true
// to false; safe to call when nothing is installed.
func teardownDNS() {
	_ = services.Mgr.Stop("servlo-dns")
	_ = services.Mgr.RemoveContainerUnit("servlo-dns")
	_ = services.Mgr.DaemonReload()

	// Only when servlo actually wrote resolver config. install.go calls this on
	// every run where DNS is off, not just on a true->false flip, so an
	// unconditional teardown would revert interfaces and restart NetworkManager on
	// every `servlo install` for someone who never let servlo near their resolver.
	if !dnsResolverConfigured() {
		return
	}
	// Announced with the lock glyph: the removals run as root. They are granted in
	// the sudoers drop-in so they do not prompt, but the header keeps the teardown
	// visible in the output.
	feedback.Sudo("Removing DNS configuration")
	dnsTeardown()
}

// Seams so tests can drive the disable path without shelling out to sudo or
// depending on what the test host happens to have installed.
var (
	dnsTeardown           = dns.Teardown
	dnsResolverConfigured = dns.ResolverConfigured
)
