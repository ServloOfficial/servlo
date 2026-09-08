package authz

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Attach a panel domain, which is the deployment servlo recommends, and every
// remote request arrives through servlo's own nginx with nginx's peer address.
// Counting against that gives the whole internet one rate-limit bucket: an
// attacker guessing passwords locks out every operator instead of themselves,
// and the audit log answers "who signed in from where" with the proxy for every
// remote sign-in.
func TestSourceAddress_UsesTheClientAddressBehindServlosOwnNginx(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "@" // a unix socket peer has no address of its own
	req.Header.Set("X-Real-IP", "203.0.113.9")
	req = req.WithContext(WithOwnProxy(req.Context()))

	if got := sourceAddress(req); got != "203.0.113.9" {
		t.Errorf("sourceAddress = %q, want the client's address rather than the proxy's", got)
	}
}

// The whole reason the peer address was the only thing trusted: a header is set
// by whoever is calling. A direct request may not talk servlo out of its own
// idea of where it came from, or an attacker resets their budget every request.
func TestSourceAddress_IgnoresTheHeaderOnADirectRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "198.51.100.4:41234"
	req.Header.Set("X-Real-IP", "127.0.0.1")

	if got := sourceAddress(req); got != "198.51.100.4" {
		t.Errorf("sourceAddress = %q, want the peer address: a direct client cannot be believed", got)
	}
}

// A vhost that has not been regenerated since this landed, or a header that is
// not an address, loses the precision rather than the limiting.
func TestSourceAddress_FallsBackToThePeerWhenTheHeaderIsUnusable(t *testing.T) {
	for _, value := range []string{"", "not-an-address", "203.0.113.9, 198.51.100.4"} {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "10.0.0.5:41234"
		if value != "" {
			req.Header.Set("X-Real-IP", value)
		}
		req = req.WithContext(WithOwnProxy(req.Context()))

		if got := sourceAddress(req); got != "10.0.0.5" {
			t.Errorf("X-Real-IP %q: sourceAddress = %q, want the peer address", value, got)
		}
	}
}
