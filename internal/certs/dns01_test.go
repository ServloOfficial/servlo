package certs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/realrashid/servlo/internal/dnsprovider"
)

// fakeProvider records what was published and withdrawn, so a test can show the
// challenge really reached the zone and did not linger there.
type fakeProvider struct {
	mu      sync.Mutex
	records map[string][]string
	setErr  error
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{records: map[string][]string{}}
}

func (f *fakeProvider) Name() dnsprovider.Name { return dnsprovider.Cloudflare }

func (f *fakeProvider) SetTXT(_ context.Context, record, value string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[record] = append(f.records[record], value)
	return nil
}

func (f *fakeProvider) RemoveTXT(_ context.Context, record, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var kept []string
	for _, v := range f.records[record] {
		if v != value {
			kept = append(kept, v)
		}
	}
	if len(kept) == 0 {
		delete(f.records, record)
	} else {
		f.records[record] = kept
	}
	return nil
}

func (f *fakeProvider) valuesAt(record string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.records[record]...)
}

func withProvider(t *testing.T, p dnsprovider.Provider) {
	t.Helper()
	prev := providerFor
	t.Cleanup(func() { providerFor = prev })
	providerFor = func(dnsprovider.Name) (dnsprovider.Provider, error) { return p, nil }
}

// The DNS-01 issuer proves control through a record, not through an inbound
// connection, so gating it on the domain resolving to this server would refuse
// exactly the case it exists for: a wildcard on a host the authority never
// connects to.
func TestDNS01Issuer_NeedsNoInboundReachability(t *testing.T) {
	iss := NewACMEIssuer(ACMEConfig{Challenge: ChallengeDNS01, DNSProvider: dnsprovider.Cloudflare})
	reach, ok := iss.(interface{ NeedsInboundReachability() bool })
	if !ok {
		t.Fatal("the issuer does not declare its reachability needs")
	}
	if reach.NeedsInboundReachability() {
		t.Error("a DNS-01 issuer demanded that the domain resolve to this server")
	}
}

func TestHTTP01Issuer_StillNeedsInboundReachability(t *testing.T) {
	iss := NewACMEIssuer(ACMEConfig{})
	reach := iss.(interface{ NeedsInboundReachability() bool })
	if !reach.NeedsInboundReachability() {
		t.Error("the default issuer stopped requiring inbound reachability")
	}
}

// A wildcard can only be proved over DNS-01, so asking for one over HTTP-01 has
// to fail with that explanation rather than an authorization error from the
// authority forty seconds later.
func TestIssuer_RefusesAWildcardOverHTTP01(t *testing.T) {
	withDNSReport(t, readyReport("example.com"))
	withIssuer(t, NewACMEIssuer(ACMEConfig{DirectoryURL: "https://acme.invalid/directory"}))

	err := IssueCertForce("example.com", []string{"example.com", "*.example.com"}, t.TempDir())
	if err == nil {
		t.Fatal("a wildcard was accepted over HTTP-01")
	}
	if !strings.Contains(err.Error(), "wildcard") || !strings.Contains(err.Error(), "dns-01") {
		t.Errorf("error %q does not explain that a wildcard needs DNS-01", err)
	}
}

// A wildcard and its base share one challenge record name. Both values have to
// be present at once, or proving the second withdraws the proof of the first.
func TestDNS01_WildcardAndBaseShareOneRecordWithBothValues(t *testing.T) {
	p := newFakeProvider()
	withProvider(t, p)
	iss := &acmeIssuer{cfg: ACMEConfig{Challenge: ChallengeDNS01, DNSProvider: dnsprovider.Cloudflare}}

	cleanup, err := iss.publishDNSChallenges(context.Background(), "example.com", map[string]string{
		"example.com":   "value-base",
		"*.example.com": "value-wildcard",
	})
	if err != nil {
		t.Fatalf("publishing: %v", err)
	}

	got := p.valuesAt("_acme-challenge.example.com")
	if len(got) != 2 {
		t.Fatalf("published %v, want both the base and wildcard values at one record", got)
	}

	cleanup()
	if left := p.valuesAt("_acme-challenge.example.com"); len(left) != 0 {
		t.Errorf("challenge records left behind: %v", left)
	}
}

// A token left in the zone is a public record that outlives the attempt, and on
// the next order for the same name it is indistinguishable from the live one.
func TestDNS01_CleansUpAfterAFailedPublish(t *testing.T) {
	p := newFakeProvider()
	withProvider(t, p)
	iss := &acmeIssuer{cfg: ACMEConfig{Challenge: ChallengeDNS01, DNSProvider: dnsprovider.Cloudflare}}

	// The first record publishes, then the provider starts failing.
	cleanup, err := iss.publishDNSChallenges(context.Background(), "example.com", map[string]string{
		"a.example.com": "value-a",
	})
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}
	p.setErr = context.DeadlineExceeded
	if _, err := iss.publishDNSChallenges(context.Background(), "example.com", map[string]string{
		"b.example.com": "value-b",
	}); err == nil {
		t.Fatal("a failing provider reported success")
	}
	cleanup()
	if left := p.valuesAt("_acme-challenge.a.example.com"); len(left) != 0 {
		t.Errorf("the first record was left behind after a later failure: %v", left)
	}
}

// A DNS-01 issuer with no configured provider must say so before an order is
// placed, not after the authority has recorded a failed validation.
func TestDNS01_RefusesWithoutAConfiguredProvider(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	prev := providerFor
	t.Cleanup(func() { providerFor = prev })
	providerFor = dnsprovider.For

	iss := &acmeIssuer{cfg: ACMEConfig{Challenge: ChallengeDNS01, DNSProvider: dnsprovider.Cloudflare}}
	_, err := iss.publishDNSChallenges(context.Background(), "example.com", map[string]string{"example.com": "v"})
	if err == nil {
		t.Fatal("issuance proceeded with no provider credentials")
	}
	if !strings.Contains(err.Error(), "cloudflare") {
		t.Errorf("error %q does not name the provider that is unconfigured", err)
	}
}

func TestIssuerName_DistinguishesTheChallenge(t *testing.T) {
	http01 := NewACMEIssuer(ACMEConfig{})
	dns01 := NewACMEIssuer(ACMEConfig{Challenge: ChallengeDNS01, DNSProvider: dnsprovider.Cloudflare})
	if http01.Name() == dns01.Name() {
		t.Errorf("both issuers report %q; doctor could not tell which one is in force", http01.Name())
	}
	if !strings.Contains(dns01.Name(), "dns-01") {
		t.Errorf("the DNS-01 issuer reports %q, which does not say how it validates", dns01.Name())
	}
}

// The story's acceptance criterion, end to end: a wildcard certificate issued
// through a real ACME client against an authority that checks the TXT record
// servlo actually published, with the provider driven the whole way.
func TestDNS01_IssuesAWildcardEndToEnd(t *testing.T) {
	webroot := acmeEnv(t)
	ca := newFakeCA(t, webroot)
	p := newFakeProvider()
	withProvider(t, p)

	// The authority validates against what the provider holds, so a record
	// published under the wrong name fails here exactly as it would live.
	ca.txt = p.valuesAt
	ca.wildcard = map[string]bool{"example.com": true}

	// No propagation wait: the fake registrar is already consistent, and
	// twenty seconds of sleeping would buy the test nothing.
	prevWait := dnsPropagationWait
	t.Cleanup(func() { dnsPropagationWait = prevWait })
	dnsPropagationWait = 0

	withIssuer(t, NewACMEIssuer(ACMEConfig{
		DirectoryURL: ca.directoryURL(),
		Challenge:    ChallengeDNS01,
		DNSProvider:  dnsprovider.Cloudflare,
	}))

	dir := t.TempDir()
	if err := IssueCertForce("example.com", []string{"example.com", "*.example.com"}, dir); err != nil {
		t.Fatalf("issuing a wildcard: %v", err)
	}

	got := ca.issuedNames(t)
	if strings.Join(got, ",") != "example.com,*.example.com" {
		t.Errorf("certificate covers %v, want the base and the wildcard", got)
	}

	// Nothing left in the zone. A stale challenge record is a public value that
	// outlives the order that created it.
	if left := p.valuesAt("_acme-challenge.example.com"); len(left) != 0 {
		t.Errorf("challenge records left in the zone: %v", left)
	}

	// And the DNS gate never ran: a wildcard's whole point is that the
	// authority does not connect to this server.
	if _, statErr := os.Stat(filepath.Join(dir, "example.com.crt")); statErr != nil {
		t.Errorf("no certificate landed on disk: %v", statErr)
	}
}
