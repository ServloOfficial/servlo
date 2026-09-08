package authz

import (
	"context"
	"net"
	"net/http"
)

// Who a request is from, when servlo's own nginx is in the way.
//
// The limiter and the audit log both want the address a person is actually at.
// Reading that off a header is how an attacker resets their own budget on every
// request, so the peer address is the only safe default and that is what this
// used to be, always.
//
// It is also wrong for the deployment servlo recommends. Attach a panel domain
// and every remote request arrives through servlo's own nginx over a unix
// socket, so every one of them has the same peer address: nginx's. One bucket
// for the whole internet means an attacker guessing passwords locks out every
// operator instead of themselves, and the audit log answers "who signed in from
// where" with the proxy's address for every remote sign-in.
//
// So the header is trusted in exactly one case: the request came in over the
// socket only servlo's nginx is on. Both vhosts that use it set X-Real-IP with
// proxy_set_header, which overwrites whatever the client sent, and nothing else
// can reach the socket without filesystem access to it. A direct TCP request
// keeps its peer address and can forge nothing.

type ctxKeyOwnProxy struct{}

// WithOwnProxy marks a request as having arrived through servlo's own nginx.
// Called by the panel's unix-socket listener; nothing else may call it, because
// what it grants is belief in a header.
func WithOwnProxy(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyOwnProxy{}, true)
}

// fromOwnProxy reports whether WithOwnProxy marked this request.
func fromOwnProxy(r *http.Request) bool {
	v, _ := r.Context().Value(ctxKeyOwnProxy{}).(bool)
	return v
}

// sourceAddress is the address a request is counted and recorded against.
func sourceAddress(r *http.Request) string {
	if fromOwnProxy(r) {
		if real := realIP(r); real != "" {
			return real
		}
	}
	return peerAddress(r)
}

// realIP is the client address servlo's nginx recorded, or empty when the
// header is missing or is not an address. Empty falls back to the peer, so a
// vhost that has not been regenerated since this landed loses the precision
// rather than the limiting.
func realIP(r *http.Request) string {
	candidate := r.Header.Get("X-Real-IP")
	if candidate == "" {
		return ""
	}
	if net.ParseIP(candidate) == nil {
		return ""
	}
	return candidate
}

func peerAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
