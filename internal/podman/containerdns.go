package podman

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

// pastaDefaultForwarder is the address pasta answers DNS on inside a rootless
// container netns. It is the last resort rather than the first choice: pasta
// picks the address itself and records it, so the recorded value is the one to
// trust.
const pastaDefaultForwarder = "169.254.1.1"

// ContainerDNS returns the DNS servers the servlo network should hand its
// containers.
//
// Rootless podman puts containers in their own netns, where the host's
// nameservers are not reachable by their own addresses; pasta bridges that gap
// and records the forwarder it chose. Servlo reads that record rather than the
// host's resolver configuration, which it deliberately neither reads nor
// writes. The list is never empty, because a network created with none leaves
// containers unable to resolve anything at all.
//
// This survived the removal of the .test DNS stack because it was never part of
// it: it is what makes ordinary outbound name resolution work from a container,
// not what made a local TLD resolve.
func ContainerDNS() []string {
	path := fmt.Sprintf("/run/user/%d/containers/networks/rootless-netns/info.json", os.Getuid())
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{pastaDefaultForwarder}
	}
	var info struct {
		DnsForwardIps []string `json:"DnsForwardIps"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return []string{pastaDefaultForwarder}
	}
	var out []string
	for _, ip := range info.DnsForwardIps {
		if clean := sanitizeDNSIP(ip); clean != "" {
			out = append(out, clean)
		}
	}
	if len(out) == 0 {
		return []string{pastaDefaultForwarder}
	}
	return out
}

// sanitizeDNSIP returns ip if it is usable as an upstream DNS target inside the
// servlo container netns, or "" if it should be filtered. Loopback, unspecified
// and zoned addresses (e.g. fe80::...%18) are rejected: podman and netavark
// cannot consume scoped addresses, and link-local zones are interface-bound
// anyway.
func sanitizeDNSIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "--" {
		return ""
	}
	if strings.ContainsRune(ip, '%') {
		return ""
	}
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsLoopback() || parsed.IsUnspecified() {
		return ""
	}
	return ip
}
