package dnscheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// stubResolver answers from a table, so the check can be driven through every
// shape a real zone takes without a network.
func stubResolver(t *testing.T, table map[string][]string, failures map[string]error) {
	t.Helper()
	prev := lookupIP
	t.Cleanup(func() { lookupIP = prev })
	lookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		if err, ok := failures[host]; ok {
			return nil, err
		}
		var out []net.IP
		for _, s := range table[host] {
			out = append(out, net.ParseIP(s))
		}
		return out, nil
	}
}

func stubServerAddresses(t *testing.T, addrs ...string) {
	t.Helper()
	prev := serverAddresses
	t.Cleanup(func() { serverAddresses = prev })
	serverAddresses = func(context.Context) ([]net.IP, error) {
		var out []net.IP
		for _, s := range addrs {
			out = append(out, net.ParseIP(s))
		}
		return out, nil
	}
}

func TestCheck_ReadyWhenEveryDomainPointsHere(t *testing.T) {
	stubServerAddresses(t, "5.6.7.8")
	stubResolver(t, map[string][]string{
		"example.com":     {"5.6.7.8"},
		"www.example.com": {"5.6.7.8"},
	}, nil)

	rep := Check(context.Background(), []string{"example.com", "www.example.com"})
	if !rep.Ready() {
		t.Fatalf("both domains point here but the check is not ready: %s", rep.Summary())
	}
	if rep.Summary() != "" {
		t.Errorf("a ready report still has something to say: %q", rep.Summary())
	}
}

// The message is the whole point of the gate. An operator staring at a disabled
// button needs the two addresses side by side, not "DNS not ready".
func TestCheck_NamesTheActualMismatch(t *testing.T) {
	stubServerAddresses(t, "5.6.7.8")
	stubResolver(t, map[string][]string{"example.com": {"1.2.3.4"}}, nil)

	rep := Check(context.Background(), []string{"example.com"})
	if rep.Ready() {
		t.Fatal("a domain pointing elsewhere was reported ready")
	}
	got := rep.Summary()
	for _, want := range []string{"example.com", "1.2.3.4", "5.6.7.8"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q does not mention %q", got, want)
		}
	}
}

// One alias left behind on an old server is the ordinary way this goes wrong,
// and it has to block: the certificate covers every domain, so a single
// unresolvable alias fails the whole order.
func TestCheck_OneBadAliasBlocksTheWholeSet(t *testing.T) {
	stubServerAddresses(t, "5.6.7.8")
	stubResolver(t, map[string][]string{
		"example.com": {"5.6.7.8"},
		"old.example": {"1.2.3.4"},
	}, nil)

	rep := Check(context.Background(), []string{"example.com", "old.example"})
	if rep.Ready() {
		t.Fatal("a set with one misdirected alias was reported ready")
	}
	if !strings.Contains(rep.Summary(), "old.example") {
		t.Errorf("summary %q does not name the alias that is wrong", rep.Summary())
	}
	if strings.Contains(rep.Summary(), "example.com currently") {
		t.Errorf("summary %q complains about a domain that is fine", rep.Summary())
	}
}

// A domain with no record at all is the first state every new site is in, and
// it reads differently from one pointing at the wrong host.
func TestCheck_UnresolvableDomainSaysSo(t *testing.T) {
	stubServerAddresses(t, "5.6.7.8")
	stubResolver(t, nil, map[string]error{
		"example.com": &net.DNSError{Err: "no such host", Name: "example.com", IsNotFound: true},
	})

	rep := Check(context.Background(), []string{"example.com"})
	if rep.Ready() {
		t.Fatal("an unresolvable domain was reported ready")
	}
	if !strings.Contains(rep.Summary(), "does not resolve") {
		t.Errorf("summary %q does not distinguish a missing record from a wrong one", rep.Summary())
	}
}

// A stale A record left alongside the new one makes validation a coin flip:
// the authority picks one address and half the attempts fail. Requiring every
// record to point here turns a flaky renewal into a clear refusal.
func TestCheck_ExtraRecordElsewhereIsAMismatch(t *testing.T) {
	stubServerAddresses(t, "5.6.7.8")
	stubResolver(t, map[string][]string{"example.com": {"5.6.7.8", "1.2.3.4"}}, nil)

	rep := Check(context.Background(), []string{"example.com"})
	if rep.Ready() {
		t.Fatal("a domain with a stale second record was reported ready")
	}
	if !strings.Contains(rep.Summary(), "1.2.3.4") {
		t.Errorf("summary %q does not name the record that does not belong", rep.Summary())
	}
}

// Let's Encrypt prefers IPv6 when a AAAA record exists. A site whose A record is
// correct and whose AAAA points at an old host fails validation while looking
// perfectly healthy over IPv4, which is among the most confusing ways this can
// break.
func TestCheck_AAAAPointingElsewhereIsAMismatch(t *testing.T) {
	stubServerAddresses(t, "5.6.7.8", "2001:db8::1")
	stubResolver(t, map[string][]string{"example.com": {"5.6.7.8", "2001:db8::99"}}, nil)

	rep := Check(context.Background(), []string{"example.com"})
	if rep.Ready() {
		t.Fatal("a domain whose AAAA points elsewhere was reported ready")
	}
	if !strings.Contains(rep.Summary(), "2001:db8::99") {
		t.Errorf("summary %q does not name the AAAA record: %v", rep.Summary(), rep.Domains)
	}
}

// A server with no IPv6 of its own and a domain with no AAAA is the common
// case and must not be treated as a mismatch.
func TestCheck_IPv4OnlyIsFine(t *testing.T) {
	stubServerAddresses(t, "5.6.7.8")
	stubResolver(t, map[string][]string{"example.com": {"5.6.7.8"}}, nil)

	if rep := Check(context.Background(), []string{"example.com"}); !rep.Ready() {
		t.Errorf("an IPv4-only setup was reported unready: %s", rep.Summary())
	}
}

// Not knowing this server's own address is not the same as knowing the domain
// is wrong. Blocking on it would strand an operator behind a check that cannot
// answer, so it reports the failure and does not claim a mismatch.
func TestCheck_UnknownServerAddressExplainsItself(t *testing.T) {
	prev := serverAddresses
	t.Cleanup(func() { serverAddresses = prev })
	serverAddresses = func(context.Context) ([]net.IP, error) {
		return nil, errors.New("no usable address")
	}
	stubResolver(t, map[string][]string{"example.com": {"5.6.7.8"}}, nil)

	rep := Check(context.Background(), []string{"example.com"})
	if rep.Ready() {
		t.Fatal("the check claimed ready without knowing this server's address")
	}
	if !strings.Contains(rep.Summary(), "public address") {
		t.Errorf("summary %q does not say the server's own address is the unknown", rep.Summary())
	}
}

// Private and loopback addresses are not what a certificate authority will
// connect to, so they must never be offered as "this server is".
func TestPublicOnly_DropsAddressesNoAuthorityCanReach(t *testing.T) {
	in := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("10.0.0.5"),
		net.ParseIP("192.168.1.20"),
		net.ParseIP("172.16.5.5"),
		net.ParseIP("169.254.1.1"),
		net.ParseIP("::1"),
		net.ParseIP("fe80::1"),
		net.ParseIP("fd00::1"),
		net.ParseIP("5.6.7.8"),
		net.ParseIP("2001:db8::1"),
	}
	got := publicOnly(in)
	if len(got) != 2 {
		t.Fatalf("publicOnly kept %v, want only the two routable addresses", got)
	}
	if got[0].String() != "5.6.7.8" || got[1].String() != "2001:db8::1" {
		t.Errorf("publicOnly kept %v", got)
	}
}
