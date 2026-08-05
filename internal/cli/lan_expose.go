package cli

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/services"
)

// LANProgressFunc is invoked by EnableLANExposure / DisableLANExposure
// after every meaningful step completes. The argument is a short
// human-readable label suitable for streaming to a frontend ("Rewriting
// container quadlets", "Done — LAN IP 192.168.x.y").
// May be nil; the no-progress path is the common case (CLI without
// streaming, internal idempotent re-application from `servlo remote-setup`).
type LANProgressFunc func(step string)

// EnableLANExposure flips servlo sites from the safe loopback default to
// LAN-exposed mode. Concretely:
//
//   - persists cfg.LAN.Exposed=true
//   - exposes nginx and, when cfg.LAN.ServicesExposed is set, managed services
//   - daemon-reloads the runtime and restarts only rewritten active containers
//
// progress, if non-nil, is invoked after each step so the caller can
// stream feedback to a user (e.g. NDJSON over HTTP for the dashboard).
// Idempotent: safe to call repeatedly.
func EnableLANExposure(progress LANProgressFunc) (lanIP string, err error) {
	emit := func(step string) {
		if progress != nil {
			progress(step)
		}
	}

	emit("Saving LAN exposure flag")
	cfg, err := config.LoadGlobal()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}
	cfg.LAN.Exposed = true
	if err := config.SaveGlobal(cfg); err != nil {
		return "", fmt.Errorf("saving config: %w", err)
	}

	emit("Rewriting container quadlets")
	if err := regenerateLANContainerQuadlets(progress); err != nil {
		return "", err
	}

	emit("Detecting primary LAN IP")
	lanIP, err = detectPrimaryLANIP()
	if err != nil {
		return "", fmt.Errorf("could not auto-detect a LAN IP: %w", err)
	}

	emit("Done — servlo is reachable on " + lanIP)
	return lanIP, nil
}

// DisableLANExposure flips servlo back to the safe loopback default. Inverts
// EnableLANExposure: rewrites every container PublishPort to bind 127.0.0.1,
// rebinds every container back to loopback and
// revokes any outstanding remote-setup token (a code is only useful while
// the LAN forwarder is running). progress receives one event per step;
// pass nil for the silent path. Idempotent.
func DisableLANExposure(progress LANProgressFunc) error {
	emit := func(step string) {
		if progress != nil {
			progress(step)
		}
	}

	emit("Saving LAN exposure flag")
	cfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.LAN.Exposed = false
	if err := config.SaveGlobal(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	emit("Rewriting container quadlets")
	if err := regenerateLANContainerQuadlets(progress); err != nil {
		return err
	}

	emit("Done — servlo is loopback only")
	return nil
}

// SetManagedServiceLANExposure persists the explicit managed-service opt-in
// and reapplies the bind policy to every installed quadlet. Active services
// restart when their host bind changes; inactive services remain stopped.
func SetManagedServiceLANExposure(enabled bool, progress LANProgressFunc) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.LAN.ServicesExposed = enabled
	if err := config.SaveGlobal(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	return regenerateLANContainerQuadlets(progress)
}

// regenerateLANContainerQuadlets reapplies the current LAN bind policy to every
// installed servlo container while preserving each unit's current configuration.
// Only affected units that are already running are restarted; inactive runtime
// services remain inactive.
//
// Every unit is attempted even when one fails. Stopping at the first error
// would leave the units after it still bound to their old address while the
// config, the CLI and the dashboard all report the new one, and because the
// files on disk are already correct by then, re-running would find nothing to
// do and the drift would never clear.
func regenerateLANContainerQuadlets(progress LANProgressFunc) error {
	restart, err := podman.RebindInstalledQuadletsForLAN()
	if err != nil {
		return err
	}
	if len(restart) == 0 {
		return nil
	}

	if err := services.Mgr.DaemonReload(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	var failures []error
	for _, name := range restart {
		status, _ := services.Mgr.UnitStatus(name)
		if status != "active" && status != "activating" {
			continue
		}
		if progress != nil {
			progress("Restarting " + name)
		}
		if err := services.Mgr.Restart(name); err != nil {
			failures = append(failures, fmt.Errorf("restarting %s: %w", name, err))
		}
	}
	return errors.Join(failures...)
}

// reloadAndRestartUnit reloads the service manager and restarts the given
// unit. Used by `lan:expose` / `lan:unexpose` after rewriting a quadlet or
// unit file so the new content takes effect.
func reloadAndRestartUnit(unit string) error {
	if err := services.Mgr.DaemonReload(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := services.Mgr.Restart(unit); err != nil {
		return fmt.Errorf("restart %s: %w", unit, err)
	}
	return nil
}

// detectPrimaryLANIP returns the host's primary LAN IPv4 address.
// The UDP-dial trick is tried first; if the result comes from a VPN tunnel
// (utun/tun/tap) we fall back to scanning physical interfaces.
func detectPrimaryLANIP() (string, error) {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err == nil {
		ip := conn.LocalAddr().(*net.UDPAddr).IP
		conn.Close()
		if name, ok := interfaceNameForIP(ip); ok && !isTunnelInterface(name) {
			return ip.String(), nil
		}
		// Fell through: the route goes through a VPN tunnel — keep scanning below.
	}

	ifaces, ifErr := net.Interfaces()
	if ifErr != nil {
		return "", fmt.Errorf("listing interfaces: %w", ifErr)
	}
	// First pass: physical interfaces only (en*, eth*, wlan*).
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isTunnelInterface(iface.Name) || isContainerInterface(iface.Name) {
			continue
		}
		if ip := firstPrivateV4(iface); ip != "" {
			return ip, nil
		}
	}
	// Second pass: any non-tunnel interface as a last resort.
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isTunnelInterface(iface.Name) {
			continue
		}
		if ip := firstPrivateV4(iface); ip != "" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("no usable IPv4 address found")
}

// interfaceNameForIP returns the interface name that owns ip.
func interfaceNameForIP(ip net.IP) (string, bool) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", false
	}
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.Equal(ip) {
				return iface.Name, true
			}
		}
	}
	return "", false
}

// isTunnelInterface reports whether the interface looks like a VPN tunnel
// (macOS utun*, Linux tun*/tap*, WireGuard wg*, etc.).
func isTunnelInterface(name string) bool {
	for _, prefix := range []string{"utun", "tun", "tap", "wg", "ipsec", "ppp"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// isContainerInterface reports whether the interface belongs to a container network.
func isContainerInterface(name string) bool {
	for _, prefix := range []string{"docker", "podman", "veth", "bridge", "br-", "vf-", "vz-"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// firstPrivateV4 returns the first RFC-1918 IPv4 address on iface, or "".
func firstPrivateV4(iface net.Interface) string {
	addrs, _ := iface.Addrs()
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			v4 := ipnet.IP.To4()
			if v4 != nil && !v4.IsLoopback() && isPrivateV4(v4) {
				return v4.String()
			}
		}
	}
	return ""
}

// isPrivateV4 reports whether ip is in an RFC-1918 private range.
func isPrivateV4(ip net.IP) bool {
	private := []struct{ net, mask [4]byte }{
		{[4]byte{10, 0, 0, 0}, [4]byte{255, 0, 0, 0}},
		{[4]byte{172, 16, 0, 0}, [4]byte{255, 240, 0, 0}},
		{[4]byte{192, 168, 0, 0}, [4]byte{255, 255, 0, 0}},
	}
	for _, p := range private {
		match := true
		for i := range 4 {
			if ip[i]&p.mask[i] != p.net[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
