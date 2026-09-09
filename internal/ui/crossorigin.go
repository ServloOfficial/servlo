package ui

import (
	"net"
	"net/http"
)

// unsafeMethod reports whether m can mutate server state and therefore must
// pass the cross-origin gate. Read-only methods (GET, HEAD, OPTIONS) can't,
// so a forged one does no harm.
func unsafeMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// csrfHeader is the request header servlo's own clients set to clear the
// cross-origin gate. Its presence is the proof; the value is ignored. The
// gate reads it here and withCORS advertises it in Access-Control-Allow-Headers
// so the split-origin dashboard's preflight succeeds.
const csrfHeader = "X-Servlo-CSRF"

// csrfExemptPaths skip the cross-origin gate. The one endpoint on the list is
// reached by a non-browser client that cannot carry the header and has its own
// source protection: the internal notify bridge, POSTed over loopback by
// out-of-process CLI commands, has its own loopback gate and only triggers a
// dashboard refresh.
//
// Every entry must be a route the panel actually registers, which
// TestEveryCrossOriginExemptionIsARegisteredRoute holds it to. An exemption for
// a path nothing serves is not harmless: the laptop-bootstrap endpoint sat on
// this list through the gate's whole rewrite, unregistered on the mux since
// before it, and read to everyone after as a live endpoint with a gate of its
// own.
//
// The per-site unpause used to be exempt too, for a button on the paused-site
// holding page that POSTed here cross-origin. That page is served to whoever
// visits the site, which on a server is the public, so the button is a link to
// the dashboard now and the exemption went with it.
var csrfExemptPaths = []string{
	"/api/internal/notify",
}

func csrfExemptPath(path string) bool {
	for _, exempt := range csrfExemptPaths {
		if path == exempt {
			return true
		}
	}
	return false
}

// passesCSRF reports whether an unsafe-method request carries proof it was
// initiated by servlo's own dashboard or a trusted local client.
//
// Browsers attach Sec-Fetch-Site automatically and scripts cannot forge it:
// same-origin / same-site / none are first-party and pass; cross-site only
// passes when the Origin is one of servlo's own dashboard origins, because the
// panel-vhost-to-localhost:7073 apiBase rewrite is itself labelled
// cross-site. A real attacker's Origin is never in the allowlist.
//
// Older browsers and non-browser callers omit Sec-Fetch; for those we require
// the X-Servlo-CSRF header. A cross-origin CORS-simple request (the RCE vector)
// can't set a custom header without a preflight, and servlo only answers
// preflight for its own origins, so the header's presence is proof enough.
func passesCSRF(r *http.Request) bool {
	// The socket shortcut is for the local dashboard vhost. A request that
	// arrived over the panel's public domain uses the same socket and gets no
	// such pass: it is a browser on the internet like any other.
	if v, _ := r.Context().Value(ctxKeyUnixSocket{}).(bool); v && !fromPublicPanel(r) {
		return true
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "same-site", "none":
		return true
	case "cross-site":
		return allowedCORSOrigins[r.Header.Get("Origin")]
	}
	return r.Header.Get(csrfHeader) != ""
}

// withCrossOriginGate refuses a state-changing request that cannot show it was
// initiated by servlo's own dashboard.
//
// It had a broader name when it also decided who could reach the panel from
// off the machine: a LAN exposure flag, an HTTP Basic challenge,
// and a list of routes a remote client could not reach whatever its password,
// on the reasoning that being at the machine is itself a credential. That
// reasoning belongs to a local development tool. Servlo's panel is reached over
// the internet by design, so a rule that only an operator sitting at the
// droplet may add a site or open a database is a rule nobody can satisfy.
// withPanelAuth answers who may connect now, and roles, a permission declared
// per route and the audit log answer what they may do, the same wherever the
// request came from.
//
// What is left is the part sessions do not answer, and the name says so.
func withCrossOriginGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS preflight: pass through. A browser sends it before it has
		// anything to prove with, and it changes nothing on its own.
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// A state-changing request must prove it came from servlo's own
		// dashboard rather than a malicious page open in the operator's
		// browser. This applies to loopback too, because the RCE vector is
		// exactly a local browser POSTing to 127.0.0.1:7073/api/sites/<d>/<a>.
		// The exempt path is reached by a non-browser client that has its own
		// source protection.
		if unsafeMethod(r.Method) && !csrfExemptPath(r.URL.Path) && !passesCSRF(r) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Forbidden — cross-origin request blocked. Use the Servlo dashboard itself.", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleAccessMode serves /api/access-mode.
//
// Sites are served on every interface, always: a server panel whose web server
// answers only itself is a server nobody can reach. Nothing decides that any
// more, so the route reports it as a fact and the panel no longer asks. It is
// kept for a browser still running a cached bundle that does.
func handleAccessMode(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"sites_served": true})
}

// uiPrimaryIP reports the address the kernel would source outbound traffic
// from, which is the one a phone on the same network can reach the panel on.
func uiPrimaryIP() string {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err == nil {
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String()
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if v4 := ipnet.IP.To4(); v4 != nil && !v4.IsLoopback() {
					return v4.String()
				}
			}
		}
	}
	return ""
}

// proxyHeaders are the headers a reverse proxy adds when it forwards a request
// on someone else's behalf. Their presence is what distinguishes a proxied
// request from a browser on the machine itself, since both arrive from 127.0.0.1.
var proxyHeaders = []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip", "Forwarded"}

// forwardedByProxy reports whether r carries evidence of having been relayed.
func forwardedByProxy(r *http.Request) bool {
	for _, h := range proxyHeaders {
		if r.Header.Get(h) != "" {
			return true
		}
	}
	return false
}

// isLocalControlRequest reports whether a request may control the servlo host.
// Unix-socket requests are authoritative. A direct TCP request qualifies when
// its peer is loopback and it carries no forwarding headers, which rejects
// reverse proxies such as Tailscale Serve that connect from 127.0.0.1 for a
// remote browser.
//
// The Host header is deliberately not part of this: a local browser may reach
// the dashboard by the machine's own hostname (Debian and Ubuntu map it to
// 127.0.1.1) or any /etc/hosts alias, and locking those out would leave the
// local user with no way in.

// publicPanelHeader marks a request that reached the panel over its public
// domain rather than the local dashboard vhost.
//
// The unix socket is treated as local control because, until a panel domain
// existed, the only thing proxying into it was the servlo.localhost vhost, and
// RFC 6761 makes .localhost resolve to the visiting device's own loopback, so
// no remote browser could reach it. A panel domain is a real name on the public
// internet proxying into that same socket, so it says so, and this is what
// stops attaching one from handing every host action to whoever types the URL.
//
// nginx sets it with proxy_set_header, which overwrites whatever the client
// sent, so it cannot be stripped from outside. It only ever removes trust: a
// client that sets it directly is denying itself and nobody else.
const publicPanelHeader = "X-Servlo-Public-Panel"

// fromPublicPanel reports whether r arrived over the panel's public domain.
func fromPublicPanel(r *http.Request) bool {
	return r.Header.Get(publicPanelHeader) != ""
}

func isLocalControlRequest(r *http.Request) bool {
	if fromPublicPanel(r) {
		return false
	}
	if v, _ := r.Context().Value(ctxKeyUnixSocket{}).(bool); v {
		return true
	}
	if forwardedByProxy(r) {
		return false
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	ip := net.ParseIP(peer)
	return ip != nil && ip.IsLoopback()
}

// isLoopbackRequest reports whether r originates from the local host. Two paths
// qualify:
//
//  1. The connection arrived over the unix socket listener. Only host
//     processes with filesystem access to the socket can connect, so this is
//     at least as trusted as TCP loopback. The servlo.localhost nginx vhost
//     reaches servlo-panel via this path.
//  2. The TCP peer is a loopback IP (127.x, ::1). This catches direct visits
//     to http://localhost:7073 / http://127.0.0.1:7073.
//
// A third once qualified: a header carrying a per-install secret. It existed
// for a vhost that reached the panel over the podman bridge rather than a unix
// socket, whose requests therefore arrived from a non-loopback address and
// needed something to vouch for them. No vhost servlo writes has set that
// header since, and the panel listens on 0.0.0.0, so all it could still do was
// make a request from anywhere count as local.
func isLoopbackRequest(r *http.Request) bool {
	if fromPublicPanel(r) {
		return false
	}
	if v, _ := r.Context().Value(ctxKeyUnixSocket{}).(bool); v {
		return true
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if ip := net.ParseIP(peer); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}
