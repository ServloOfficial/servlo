package serverguard

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Cloud firewalls, and why servlo can only ever half-answer this.
//
// A provider's firewall sits in front of the machine. From inside it there is
// no interface to read, no rule to list and no way to tell an allowed port from
// a blocked one: a packet that never arrives is indistinguishable from a
// visitor who never came. Servlo cannot detect whether one is filtering, and
// any code claiming to would be guessing.
//
// What it can do is establish which provider this is, from the metadata service
// every one of them runs on the same link-local address, and then say exactly
// where the rules are. That is the difference between an afternoon spent
// debugging ufw and thirty seconds spent looking at the right screen, which is
// the whole of what the story is worth.

// metadataAddr is the link-local address every major provider answers on. Not
// routed, so a request to it never leaves the machine and cannot reach anything
// but the hypervisor.
const metadataAddr = "169.254.169.254"

// metadataTimeout is short on purpose. This runs inside an audit, on a server
// that may not be at a provider at all, where the address is simply dead. A
// long timeout would make the audit look hung.
const metadataTimeout = 700 * time.Millisecond

// Provider is where this server is hosted, as far as servlo can tell.
type Provider struct {
	// Name is the provider, empty when servlo could not tell.
	Name string `json:"name,omitempty"`
	// FirewallURL is where its firewall rules are edited.
	FirewallURL string `json:"firewall_url,omitempty"`
	// Detail is what to do about it, in words.
	Detail string `json:"detail"`
}

// metadataGet is the seam. Tests answer as a provider would rather than
// reaching a link-local address that either is not there or, worse, is.
var metadataGet = func(path string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), metadataTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+metadataAddr+path, nil)
	if err != nil {
		return "", false
	}
	client := &http.Client{
		Timeout: metadataTimeout,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: metadataTimeout}).DialContext,
			// No proxy. A metadata request that went through an operator's
			// HTTP proxy would send this server's identity somewhere it has no
			// business going.
			Proxy: nil,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	return strings.TrimSpace(string(body[:n])), true
}

// DetectProvider works out where this server is hosted.
func DetectProvider() Provider {
	// DigitalOcean answers a droplet id here and nothing else does.
	if _, ok := metadataGet("/metadata/v1/id"); ok {
		return Provider{
			Name:        "DigitalOcean",
			FirewallURL: "https://cloud.digitalocean.com/networking/firewalls",
			Detail: "This is a DigitalOcean droplet, so a cloud firewall may be filtering in front of it " +
				"where servlo cannot see. A port open in ufw and still unreachable is almost always this. " +
				"Check the droplet's Networking tab, and remember an inbound rule there has to allow 80 and " +
				"443 as well as SSH.",
		}
	}
	// Hetzner answers its own path, and its firewall is per-server rather than
	// per-project.
	if _, ok := metadataGet("/hetzner/v1/metadata/instance-id"); ok {
		return Provider{
			Name:        "Hetzner Cloud",
			FirewallURL: "https://console.hetzner.cloud/",
			Detail: "This is a Hetzner Cloud server, so a cloud firewall may be filtering in front of it " +
				"where servlo cannot see. Check the server's Firewalls tab if a port is open in ufw and " +
				"still unreachable.",
		}
	}
	// AWS wants a token first on IMDSv2, and the unauthenticated path answers
	// on IMDSv1 machines. Either way the id is enough to name the provider.
	if _, ok := metadataGet("/latest/meta-data/instance-id"); ok {
		return Provider{
			Name:        "AWS EC2",
			FirewallURL: "https://console.aws.amazon.com/ec2/#SecurityGroups",
			Detail: "This is an EC2 instance, so its security groups filter in front of it where servlo " +
				"cannot see. A port open in ufw and still unreachable is almost always a security group " +
				"with no inbound rule for it.",
		}
	}
	if onGCP() {
		return Provider{
			Name:        "Google Cloud",
			FirewallURL: "https://console.cloud.google.com/networking/firewalls",
			Detail: "This is a Google Cloud instance, so VPC firewall rules filter in front of it where " +
				"servlo cannot see. A port open in ufw and still unreachable is almost always a missing " +
				"ingress rule.",
		}
	}
	return Provider{
		Detail: "Servlo could not tell which provider this server is at. If one is involved, its firewall " +
			"filters in front of the machine where servlo cannot see it, and that is where to look before " +
			"anything here when a port is open in ufw and still unreachable.",
	}
}

// onGCP reads the product name the BIOS reports, which is how every tool tells
// a Google instance apart without a metadata request. The metadata service
// there needs a header, and reading a file beats a request that will 403.
func onGCP() bool {
	raw, err := os.ReadFile("/sys/class/dmi/id/product_name")
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "Google")
}
