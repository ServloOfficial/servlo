package dnscheck

import (
	"context"
	"net"
	"strings"

	"github.com/realrashid/servlo/internal/config"
)

// Which addresses this server answers on.
//
// Two features need the same answer. A certificate authority has to reach this
// machine at the domain, and a managed database provider has to be told which
// address to let through its firewall. They are the same address, and an
// operator who has already told servlo that their droplet sits behind a
// floating IP should not have to tell it again somewhere else.

// ThisServer is this server's public addresses: the ones the operator declared
// if they declared any, and the ones on its own interfaces otherwise.
//
// The declared list wins because a host behind a load balancer, a floating IP
// or plain NAT does not carry its public address on any interface, and the
// address the outside world sees is the one both callers need.
func ThisServer(ctx context.Context) ([]net.IP, error) {
	cfg, err := config.LoadGlobal()
	if err == nil {
		var declared []net.IP
		for _, s := range cfg.ACMEServerAddresses() {
			if ip := net.ParseIP(strings.TrimSpace(s)); ip != nil {
				declared = append(declared, ip)
			}
		}
		if len(declared) > 0 {
			return declared, nil
		}
	}
	return serverAddresses(ctx)
}

// ThisServerStrings is the same in the form a JSON response and a printed line
// both take.
func ThisServerStrings(ctx context.Context) ([]string, error) {
	ips, err := ThisServer(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out, nil
}
