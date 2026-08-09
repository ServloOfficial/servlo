package dnscheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// The address an operator pastes into a provider's trusted-sources list is the
// same address a certificate authority would connect to, so it is read from one
// place rather than worked out twice.
func TestThisServer_ReadsTheInterfaceAddresses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stubServerAddresses(t, "203.0.113.10")

	got, err := ThisServer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].String() != "203.0.113.10" {
		t.Errorf("addresses = %v, want the interface address", got)
	}
}

// A droplet behind a load balancer or a floating IP is not the address on its
// own interface, and the operator has already told servlo so for certificates.
// Asking them a second time, in a second place, is how the two disagree.
func TestThisServer_PrefersTheDeclaredAddresses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stubServerAddresses(t, "10.0.0.5")

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Certs.ServerAddresses = []string{"198.51.100.7", " 198.51.100.8 "}
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}

	got, err := ThisServer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ip := range got {
		out = append(out, ip.String())
	}
	if strings.Join(out, ",") != "198.51.100.7,198.51.100.8" {
		t.Errorf("addresses = %v, want the declared ones", out)
	}
}

// ThisServerStrings is what a JSON response and a printed line both want, and
// an install that cannot work out its own address says so rather than showing
// an empty list that reads as "no restriction needed".
func TestThisServerStrings_ReportsTheFailure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stubServerAddressesError(t, "no public address on any interface")

	addrs, err := ThisServerStrings(context.Background())
	if len(addrs) != 0 {
		t.Errorf("addresses = %v, want none", addrs)
	}
	if err == nil || !strings.Contains(err.Error(), "public address") {
		t.Errorf("error = %v, want it to say the address could not be worked out", err)
	}
}

// stubServerAddressesError makes the interface read fail, the state a host with
// nothing but private addresses is in.
func stubServerAddressesError(t *testing.T, message string) {
	t.Helper()
	prev := serverAddresses
	t.Cleanup(func() { serverAddresses = prev })
	serverAddresses = func(context.Context) ([]net.IP, error) { return nil, errors.New(message) }
}
