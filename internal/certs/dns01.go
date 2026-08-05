package certs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/realrashid/servlo/internal/dnsprovider"
)

// DNS-01, and what it buys.
//
// HTTP-01 proves control by serving a file, which means the authority has to
// reach this server on port 80 at that exact name. That cannot prove a wildcard
// at all: there is no single name to fetch for "*.example.com". DNS-01 proves
// control by publishing a TXT record, so it covers wildcards, and it works for
// a server the authority could never connect to.
//
// The cost is the credential. A DNS API token can rewrite the zone, so it is
// worth using only when a wildcard actually needs it.

// Challenge is how an issuer proves control of a domain.
type Challenge string

const (
	ChallengeHTTP01 Challenge = "http-01"
	ChallengeDNS01  Challenge = "dns-01"
)

// providerFor resolves the DNS provider client, a seam so the DNS-01 path can
// be tested without a registrar.
var providerFor = dnsprovider.For

// dnsPropagationWait is how long to let a published record spread before telling
// the authority to look. Registrars answer their own API immediately and their
// nameservers a moment later, and an authority that checks too early records a
// failed validation that counts against the rate limit.
var dnsPropagationWait = 20 * time.Second

// IsWildcard reports whether a domain can only be proved over DNS-01.
func IsWildcard(domain string) bool { return strings.HasPrefix(domain, "*.") }

// anyWildcard reports whether a domain set contains one.
func anyWildcard(domains []string) bool {
	for _, d := range domains {
		if IsWildcard(d) {
			return true
		}
	}
	return false
}

// publishDNSChallenges writes every challenge value and returns the cleanup that
// withdraws them.
//
// The values are keyed by domain, and several domains can share a record name:
// "*.example.com" and "example.com" are both proved at
// _acme-challenge.example.com, so both values have to sit there at once.
// Publishing the second as a replacement would withdraw the proof of the first
// and fail half the order.
//
// Cleanup withdraws whatever was published, including on the failure path.
// A token left in a zone is a public record that outlives the attempt that
// created it, and on the next order for the same name it is indistinguishable
// from the live one.
func (a *acmeIssuer) publishDNSChallenges(ctx context.Context, primary string, values map[string]string) (cleanup func(), err error) {
	provider, err := providerFor(a.cfg.DNSProvider)
	if err != nil {
		return func() {}, fmt.Errorf("cannot prove %s over dns-01: %w", primary, err)
	}

	type published struct{ record, value string }
	var done []published
	cleanup = func() {
		for _, p := range done {
			// Best effort: a record that cannot be withdrawn is worth reporting
			// but must not fail an issuance that otherwise succeeded.
			if rmErr := provider.RemoveTXT(context.WithoutCancel(ctx), p.record, p.value); rmErr != nil {
				noteProgress(primary, "could not withdraw the challenge record for %s: %v", p.record, rmErr)
			}
		}
	}

	for domain, value := range values {
		record := dnsprovider.ChallengeRecordName(domain)
		if err := provider.SetTXT(ctx, record, value); err != nil {
			return cleanup, fmt.Errorf("publishing the challenge for %s at %s: %w", domain, record, err)
		}
		done = append(done, published{record: record, value: value})
		noteProgress(primary, "published the challenge record for %s", domain)
	}
	return cleanup, nil
}

// satisfyDNS01 answers every pending authorization in one pass.
//
// Unlike HTTP-01, the records go up together and are validated together. The
// propagation wait is paid once for the whole order rather than once per
// domain, which for a site with several aliases is the difference between
// twenty seconds and two minutes.
func (a *acmeIssuer) satisfyDNS01(ctx context.Context, client *acme.Client, primary string, order *acme.Order) error {
	type pending struct {
		authz *acme.Authorization
		chal  *acme.Challenge
	}
	var work []pending
	values := map[string]string{}

	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return fmt.Errorf("reading an authorization: %w", err)
		}
		if authz.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, c := range authz.Challenges {
			if c.Type == "dns-01" {
				chal = c
				break
			}
		}
		if chal == nil {
			return fmt.Errorf("%s: the authority offered no dns-01 challenge", authz.Identifier.Value)
		}
		value, err := client.DNS01ChallengeRecord(chal.Token)
		if err != nil {
			return fmt.Errorf("%s: building the challenge record: %w", authz.Identifier.Value, err)
		}
		// The wildcard flag lives on the authorization, not the identifier, so
		// the name here is the base domain either way. That is what the record
		// is named after, so no reconstruction is needed.
		values[authz.Identifier.Value] = value
		work = append(work, pending{authz: authz, chal: chal})
	}
	if len(work) == 0 {
		return nil
	}

	cleanup, err := a.publishDNSChallenges(ctx, primary, values)
	defer cleanup()
	if err != nil {
		return err
	}

	noteProgress(primary, "waiting %s for the records to propagate", dnsPropagationWait)
	select {
	case <-time.After(dnsPropagationWait):
	case <-ctx.Done():
		return ctx.Err()
	}

	for _, w := range work {
		if _, err := client.Accept(ctx, w.chal); err != nil {
			return fmt.Errorf("%s: %w", w.authz.Identifier.Value, dnsValidationHint(err))
		}
	}
	for _, w := range work {
		if _, err := client.WaitAuthorization(ctx, w.authz.URI); err != nil {
			return fmt.Errorf("%s: %w", w.authz.Identifier.Value, dnsValidationHint(err))
		}
		noteProgress(primary, "%s verified", w.authz.Identifier.Value)
	}
	return nil
}

// asACMEError is errors.As specialised to the authority's problem document.
func asACMEError(err error, target **acme.Error) bool { return errors.As(err, target) }

// dnsValidationHint names the causes an operator can actually act on. A DNS-01
// failure is nearly always the record not having spread yet, or the credential
// holding a different zone than the domain lives in.
func dnsValidationHint(err error) error {
	var problem *acme.Error
	if asACMEError(err, &problem) {
		return fmt.Errorf("the authority could not find the challenge record (%s). "+
			"Either it has not propagated yet, or the credentials servlo holds are for a different zone than this domain: %w",
			problem.ProblemType, err)
	}
	return err
}
