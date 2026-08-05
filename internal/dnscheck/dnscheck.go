// Package dnscheck answers one question before servlo asks a certificate
// authority for anything: do these domains actually point at this server?
//
// It exists because getting that wrong is expensive. Let's Encrypt locks an
// account out of retrying a domain after five failed validations, so an
// operator who clicks Get SSL against a record that has not propagated burns
// their production quota learning what a DNS lookup would have told them for
// free. Checking first turns a week-long lockout into a sentence on screen.
package dnscheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// lookupTimeout bounds the whole check. It runs behind a panel request and on
// the way into an issuance, so a nameserver that never answers must not hold
// either open.
const lookupTimeout = 5 * time.Second

// Seams. Both are package vars so the check can be driven through every zone
// shape in tests without a network or a routable address.
var (
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	}
	serverAddresses = defaultServerAddresses
)

// Result is one domain's answer.
type Result struct {
	Domain string `json:"domain"`
	// Resolved is every address the domain currently points at, in the order
	// the resolver returned them.
	Resolved []string `json:"resolved"`
	// Elsewhere is the subset of Resolved that is not this server. It is what
	// the operator has to change, so it is called out rather than left to be
	// worked out from the two lists.
	Elsewhere []string `json:"elsewhere,omitempty"`
	Matched   bool     `json:"matched"`
	// Error is set when the domain could not be resolved at all, which reads
	// differently from resolving to the wrong place.
	Error string `json:"error,omitempty"`
}

// Report is the whole domain set measured against this server.
type Report struct {
	// Server is this server's public addresses, the ones an authority could
	// actually connect to.
	Server  []string `json:"server"`
	Domains []Result `json:"domains"`
	// Error is set when servlo could not work out its own public address, which
	// is a failure of the check rather than a verdict about the domains.
	Error string `json:"error,omitempty"`
}

// Ready reports whether every domain resolves here and nothing else.
func (r Report) Ready() bool {
	if r.Error != "" || len(r.Domains) == 0 {
		return false
	}
	for _, d := range r.Domains {
		if !d.Matched {
			return false
		}
	}
	return true
}

// Summary is what the panel shows beside a disabled button and what the CLI
// prints when it refuses. Empty when everything is ready.
func (r Report) Summary() string {
	if r.Error != "" {
		return "Waiting for DNS — servlo could not determine this server's public address: " + r.Error
	}
	server := strings.Join(r.Server, ", ")
	var parts []string
	for _, d := range r.Domains {
		switch {
		case d.Matched:
			continue
		case d.Error != "":
			parts = append(parts, fmt.Sprintf("%s does not resolve yet (%s)", d.Domain, d.Error))
		case len(d.Resolved) == 0:
			parts = append(parts, fmt.Sprintf("%s does not resolve yet", d.Domain))
		default:
			parts = append(parts, fmt.Sprintf("%s currently resolves to %s", d.Domain, strings.Join(d.Elsewhere, ", ")))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("Waiting for DNS — %s, this server is %s", strings.Join(parts, "; "), server)
}

// ServerAddresses reports this server's public addresses, read from its own
// interfaces.
func ServerAddresses(ctx context.Context) ([]net.IP, error) { return serverAddresses(ctx) }

// Check resolves every domain and compares it against this server's own
// interfaces.
func Check(ctx context.Context, domains []string) Report {
	mine, err := serverAddresses(ctx)
	if err != nil {
		return Report{Error: err.Error()}
	}
	return CheckAgainst(ctx, domains, mine)
}

// CheckAgainst compares domains against an address set the caller supplies.
//
// It exists for the server whose public address is not on any of its own
// interfaces: behind a cloud load balancer, a floating IP, or plain NAT with a
// port forward. Such a host can answer an HTTP-01 challenge perfectly well, and
// deciding from interface addresses alone would lock it out of certificates
// entirely, so the operator can name the address instead.
func CheckAgainst(ctx context.Context, domains []string, mine []net.IP) Report {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	rep := Report{}
	if len(mine) == 0 {
		rep.Error = "no public address for this server"
		return rep
	}
	for _, ip := range mine {
		rep.Server = append(rep.Server, ip.String())
	}
	for _, domain := range domains {
		rep.Domains = append(rep.Domains, checkOne(ctx, domain, mine))
	}
	return rep
}

func checkOne(ctx context.Context, domain string, mine []net.IP) Result {
	res := Result{Domain: domain}
	ips, err := lookupIP(ctx, domain)
	if err != nil {
		res.Error = resolveError(err)
		return res
	}
	for _, ip := range ips {
		res.Resolved = append(res.Resolved, ip.String())
		if !containsIP(mine, ip) {
			res.Elsewhere = append(res.Elsewhere, ip.String())
		}
	}
	// Every record has to point here, not merely one of them. A stale address
	// left alongside the new one makes validation a coin flip, because the
	// authority connects to whichever it picks, and a renewal that fails half
	// the time is harder to diagnose than one that never runs.
	res.Matched = len(res.Resolved) > 0 && len(res.Elsewhere) == 0
	return res
}

// resolveError renders a lookup failure the way an operator reads it. The Go
// error text for a missing record is already plain enough; anything else is
// reported as-is rather than guessed at.
func resolveError(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return "no DNS record"
	}
	return err.Error()
}

func containsIP(set []net.IP, ip net.IP) bool {
	for _, candidate := range set {
		if candidate.Equal(ip) {
			return true
		}
	}
	return false
}

// defaultServerAddresses reports this server's public addresses from its own
// interfaces. On a droplet the public address is bound directly, so this needs
// no network call and cannot be wrong about which host it is describing.
//
// It is also where the managed-database flow will read the address it has to
// show for a provider's trusted-sources list.
func defaultServerAddresses(context.Context) ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			ips = append(ips, ipnet.IP)
		}
	}
	public := publicOnly(ips)
	if len(public) == 0 {
		return nil, fmt.Errorf("no public address on any interface; a server behind NAT cannot answer an HTTP-01 challenge without a port forward")
	}
	return public, nil
}

// publicOnly keeps the addresses a certificate authority could actually
// connect to. Loopback, link-local and the private ranges are dropped: offering
// one as "this server is" would send an operator chasing a record that could
// never work.
func publicOnly(ips []net.IP) []net.IP {
	var out []net.IP
	for _, ip := range ips {
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
			continue
		}
		out = append(out, ip)
	}
	return out
}
