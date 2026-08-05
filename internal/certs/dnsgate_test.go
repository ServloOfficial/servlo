package certs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dnscheck"
)

// withDNSReport swaps the live check for a fixed answer, so the gate can be
// driven without a resolver.
func withDNSReport(t *testing.T, rep dnscheck.Report) {
	t.Helper()
	prev := checkDNS
	t.Cleanup(func() { checkDNS = prev })
	checkDNS = func(context.Context, []string) dnscheck.Report { return rep }
}

func readyReport(domains ...string) dnscheck.Report {
	rep := dnscheck.Report{Server: []string{"5.6.7.8"}}
	for _, d := range domains {
		rep.Domains = append(rep.Domains, dnscheck.Result{Domain: d, Resolved: []string{"5.6.7.8"}, Matched: true})
	}
	return rep
}

func misdirectedReport(domain string) dnscheck.Report {
	return dnscheck.Report{
		Server: []string{"5.6.7.8"},
		Domains: []dnscheck.Result{{
			Domain: domain, Resolved: []string{"1.2.3.4"}, Elsewhere: []string{"1.2.3.4"},
		}},
	}
}

// reachingIssuer is a recordingIssuer whose challenge needs the authority to
// connect back, which is what puts it behind the gate.
type reachingIssuer struct{ recordingIssuer }

func (*reachingIssuer) NeedsInboundReachability() bool { return true }

// The gate is what keeps a misconfigured record from spending the production
// rate limit. Five failed validations lock the domain out for an hour, so an
// issuance that was never going to succeed must not reach the authority.
func TestIssueCertForce_RefusesWhileDNSPointsElsewhere(t *testing.T) {
	iss := &reachingIssuer{recordingIssuer{name: "test"}}
	withIssuer(t, iss)
	rec := &iss.recordingIssuer
	withDNSReport(t, misdirectedReport("example.com"))

	err := IssueCertForce("example.com", []string{"example.com"}, t.TempDir())
	if err == nil {
		t.Fatal("issuance went ahead with DNS pointing elsewhere")
	}
	if len(rec.calls) != 0 {
		t.Error("the authority was contacted despite the domain not pointing here")
	}
	if !errors.Is(err, ErrDNSNotReady) {
		t.Errorf("error %v is not recognisable as the DNS gate", err)
	}
	for _, want := range []string{"example.com", "1.2.3.4", "5.6.7.8"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestIssueCertForce_ProceedsWhenDNSPointsHere(t *testing.T) {
	iss := &reachingIssuer{recordingIssuer{name: "test"}}
	withIssuer(t, iss)
	rec := &iss.recordingIssuer
	withDNSReport(t, readyReport("example.com", "www.example.com"))

	if err := IssueCertForce("example.com", []string{"example.com", "www.example.com"}, t.TempDir()); err != nil {
		t.Fatalf("IssueCertForce: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Errorf("issuer called %d times, want 1", len(rec.calls))
	}
}

// The gate belongs to the authority, not to certificates in general. An issuer
// that needs no inbound connection has nothing to prove with a DNS record, and
// gating it would break the tests and the local flows that use one.
func TestDNSGate_OnlyAppliesToIssuersThatNeedInboundReachability(t *testing.T) {
	withDNSReport(t, misdirectedReport("example.com"))
	withIssuer(t, &recordingIssuer{name: "test"})

	if err := IssueCertForce("example.com", []string{"example.com"}, t.TempDir()); err != nil {
		t.Fatalf("a non-ACME issuer was gated on DNS: %v", err)
	}
}

// The check runs once per issuance, not once per domain: a site with six
// aliases should not make six passes over the same report.
func TestIssueCertForce_ChecksDNSOncePerIssuance(t *testing.T) {
	withIssuer(t, &acmeIssuer{})
	calls := 0
	prev := checkDNS
	t.Cleanup(func() { checkDNS = prev })
	checkDNS = func(_ context.Context, domains []string) dnscheck.Report {
		calls++
		return readyReport(domains...)
	}

	// The issuance itself fails (there is no authority at that URL); what is
	// under test is how many times the gate ran before it got there.
	_ = IssueCertForce("example.com", []string{"example.com", "a.example.com", "b.example.com"}, t.TempDir())
	if calls != 1 {
		t.Errorf("the DNS check ran %d times for one issuance, want 1", calls)
	}
}

// A host whose public address is not on any of its own interfaces answers an
// HTTP-01 challenge perfectly well: behind a load balancer, a floating IP, or
// NAT with a port forward. Reading interfaces alone would lock it out of
// certificates for a reason that is not true, so the operator can say what the
// authority will actually connect to.
func TestCheckDNS_HonoursTheDeclaredServerAddress(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.Certs.ServerAddresses = []string{"5.6.7.8"}
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	rep := checkDNS(context.Background(), []string{"example.invalid"})
	if len(rep.Server) != 1 || rep.Server[0] != "5.6.7.8" {
		t.Errorf("server addresses = %v, want the declared one", rep.Server)
	}
	// It got as far as looking the domain up, which is the point: without the
	// declaration the check would have stopped before this.
	if len(rep.Domains) != 1 {
		t.Errorf("checked %d domains, want the one asked for", len(rep.Domains))
	}
}
