package certs

import (
	"context"
	"errors"
	"fmt"

	"github.com/ServloOfficial/servlo/internal/dnscheck"
)

// ErrDNSNotReady means the domains do not resolve to this server yet, so no
// authority validating over an inbound connection could succeed. Callers match
// on it to show the mismatch rather than a generic failure.
var ErrDNSNotReady = errors.New("domains do not resolve to this server")

// checkDNS is the live lookup, a seam so the gate can be tested without a
// resolver. It honours the configured server addresses, so a host behind a
// load balancer or a floating IP is measured against the address the authority
// will actually connect to rather than the one on its interface.
var checkDNS = func(ctx context.Context, domains []string) dnscheck.Report {
	mine, err := dnscheck.ThisServer(ctx)
	if err != nil {
		return dnscheck.Report{Error: err.Error()}
	}
	return dnscheck.CheckAgainst(ctx, domains, mine)
}

// challengeReachability is implemented by an issuer that says whether its
// challenge needs the authority to reach this server at the domain. HTTP-01
// does. DNS-01 proves control through a TXT record and does not, which is why
// this is the issuer's answer rather than a rule baked into the swap.
type challengeReachability interface {
	NeedsInboundReachability() bool
}

// guardDNS refuses an issuance whose challenge could not possibly succeed.
//
// This is the difference between a sentence on screen and a week of waiting.
// Let's Encrypt locks an account out of retrying a domain after five failed
// validations, so an operator clicking Get SSL against a record that has not
// propagated would spend their production quota discovering what one DNS
// lookup answers for free.
//
// It runs once per issuance rather than once per domain, and not at all on the
// reuse path, so an ordinary renewal pass over fifty sites does no lookups for
// the ones that do not need renewing.
func guardDNS(iss Issuer, primary string, domains []string) error {
	needs, ok := iss.(challengeReachability)
	if !ok || !needs.NeedsInboundReachability() {
		return nil
	}
	report := checkDNS(context.Background(), domains)
	if report.Ready() {
		return nil
	}
	return fmt.Errorf("cannot issue a certificate for %s: %w. %s", primary, ErrDNSNotReady, report.Summary())
}

// DNSReport is the live check for a site's domains, for the panel to render
// beside the Get SSL button and for the CLI to print when it refuses.
func DNSReport(ctx context.Context, domains []string) dnscheck.Report {
	return checkDNS(ctx, domains)
}
