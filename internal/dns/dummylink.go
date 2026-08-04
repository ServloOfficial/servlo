package dns

import "os"

// servlo0 is the always-up dummy link that keeps .test resolving when every real
// network link is down. Only the Linux NetworkManager + systemd-resolved path
// provisions it (see setup.go); macOS resolves .test through /etc/resolver and
// needs no equivalent. The names live here rather than in the linux-only setup
// file because the diagnostic chain is built for every platform and reports on
// the link by name.
const (
	servloDummyIface   = "servlo0"
	servloLinkUnitName = "servlo-dns-link.service"
	servloLinkUnit     = "/etc/systemd/system/servlo-dns-link.service"
)

// ResolverConfigured reports whether servlo has written any resolver config on this
// host. Teardown is destructive (it reverts interfaces and restarts the network
// stack) and needs a password, so callers that run on every install must ask this
// first rather than tearing down a resolver servlo never touched.
func ResolverConfigured() bool {
	for _, p := range resolverArtifacts {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// resolverArtifacts is every path servlo's resolver setup writes on Linux, across
// all three resolver paths. macOS writes /etc/resolver/<tld> instead and tears
// its own files down in install_dns_darwin.go, so it never consults this.
var resolverArtifacts = []string{
	"/etc/NetworkManager/dispatcher.d/99-servlo-dns",
	"/etc/NetworkManager/conf.d/servlo.conf",
	"/etc/NetworkManager/dnsmasq.d/servlo.conf",
	"/etc/NetworkManager/conf.d/servlo-dns-link.conf",
	"/etc/systemd/resolved.conf.d/servlo.conf",
	"/etc/systemd/resolved.conf.d/servlo-fallback.conf",
	servloLinkUnit,
}
