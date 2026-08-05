package dnsprovider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Provider publishes and withdraws the TXT records a DNS-01 challenge needs.
//
// Only TXT, and only under _acme-challenge: this is not a DNS management API
// and must not grow into one. The credential behind it can rewrite the whole
// zone, so the narrower the surface servlo exposes, the smaller the blast
// radius of a bug in it.
type Provider interface {
	// SetTXT publishes value at the fully qualified record name, adding to any
	// values already there rather than replacing them. A wildcard and its base
	// domain share one record name and need both values present at once.
	SetTXT(ctx context.Context, record, value string) error
	// RemoveTXT withdraws one value, leaving any others in place.
	RemoveTXT(ctx context.Context, record, value string) error
	Name() Name
}

// httpTimeout bounds one API call. A registrar that hangs must not hold an
// issuance open until the ACME client's own deadline expires.
const httpTimeout = 30 * time.Second

// ChallengeRecordName returns where the challenge for a domain is published.
// Fixed by RFC 8555, and the single easiest thing to get wrong: a value at the
// domain itself rather than under _acme-challenge fails validation with nothing
// useful in the authority's error.
//
// A wildcard resolves to its base, because "*.example.com" and "example.com"
// are proved by the same record name. An order covering both publishes two
// values there, which is why SetTXT adds rather than replaces.
func ChallengeRecordName(domain string) string {
	return "_acme-challenge." + strings.TrimPrefix(domain, "*.")
}

// longestZoneSuffix picks the zone a record belongs to. Longest match wins, so a
// provider hosting both example.com and bar.example.com puts a record under
// foo.bar.example.com in the more specific one, which is where its nameservers
// actually answer.
func longestZoneSuffix(record string, zones []string) (string, error) {
	best := ""
	for _, zone := range zones {
		z := strings.TrimSuffix(zone, ".")
		if record == z || strings.HasSuffix(record, "."+z) {
			if len(z) > len(best) {
				best = z
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("no zone on this account covers %s; check the domain is hosted by the provider whose credentials servlo holds", record)
	}
	return best, nil
}

// relativeName renders a record as the provider wants it when it names records
// relative to the zone. Sending the fully qualified name to such an API creates
// _acme-challenge.example.com.example.com, which validates against nothing.
func relativeName(record, zone string) string {
	trimmed := strings.TrimSuffix(record, "."+zone)
	if trimmed == record {
		return "@"
	}
	return trimmed
}

// For returns the provider client for a configured provider.
func For(name Name) (Provider, error) {
	creds, err := LoadCredentials(name)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: httpTimeout}
	switch name {
	case Cloudflare:
		return &cloudflareProvider{token: creds.APIToken, baseURL: cloudflareAPI, client: client}, nil
	case DigitalOcean:
		return &digitalOceanProvider{token: creds.APIToken, baseURL: digitalOceanAPI, client: client}, nil
	case Route53:
		return &route53Provider{creds: creds, baseURL: route53API, client: client}, nil
	}
	return nil, fmt.Errorf("unknown DNS provider %q", name)
}
