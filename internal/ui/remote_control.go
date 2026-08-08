package ui

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	servlocli "github.com/realrashid/servlo/internal/cli"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/nginx"
	"golang.org/x/crypto/bcrypt"
)

// fromHost reports whether r's source IP belongs to one of the host's
// own interfaces. The mailpit container reaches the dashboard via
// host.containers.internal, which pasta (Linux) and gvproxy / vmnet
// (macOS) source-NAT to the host, so servlo-panel sees the request as coming
// from one of its own addresses. A LAN attacker arrives from a different
// IP and is rejected. Spoofing a host-owned address would break the TCP
// handshake because the SYN-ACK routes back into the host rather than
// reaching the attacker.
//
// Interfaces are re-read on every call so VPN attach, WiFi switch, or
// a late-arriving podman bridge are picked up without a daemon restart.
// Each call is a few syscalls, fine for webhook-rate traffic.
func fromHost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// IPv6 link-local sources may carry a zone suffix (fe80::1%eth0);
	// strip it before parsing so the value compare below works.
	if i := strings.Index(host, "%"); i != -1 {
		host = host[:i]
	}
	src := net.ParseIP(host)
	if src == nil {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		if src.Equal(ipNet.IP) {
			return true
		}
	}
	return false
}

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

// csrfExemptPath reports whether path skips the cross-origin gate. These
// endpoints are reached by non-browser clients (or cross-origin pages we
// can't control) that can't carry the header, and each already has its own
// source protection: /api/remote-setup has a token + RFC1918 + lockout gate,
// the mailpit webhook is restricted to host-NAT'd source IPs, the internal
// notify bridge (POSTed over loopback by out-of-process CLI commands) has its
// own loopback gate and only triggers a dashboard refresh.
//
// The per-site unpause used to be exempt too, for a button on the paused-site
// holding page that POSTed here cross-origin. That page is served to whoever
// visits the site, which on a server is the public, so the button is a link to
// the dashboard now and the exemption went with it.
func csrfExemptPath(path string) bool {
	switch path {
	case "/api/remote-setup", "/api/webhooks/mailpit", "/api/internal/notify":
		return true
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

// withRemoteControlGate is what is left of the gate after S5.5.
//
// Authentication is no longer its job. withPanelAuth sits in front and refuses
// anything without a session, whatever address it came from, so the LAN
// exposure flag and the HTTP Basic challenge that used to live here are gone
// along with the model that needed them: on a server there is no trusted side
// of the connection to exempt.
//
// Authority is no longer its job either. It used to hold a list of routes a
// remote client could not reach whatever its password, on the reasoning that
// being at the machine is itself a credential. That reasoning belongs to a
// local development tool. Servlo's panel is reached over the internet by
// design, and a rule that only an operator sitting at the droplet may add a
// site or open a database is a rule that nobody can satisfy, so the whole
// surface was hidden from the only person who was ever going to use it. Roles
// (S5.4), a permission declared per route (S5.5) and the audit log (S5.6) are
// what answer the question now, and they answer it the same wherever the
// request came from.
//
// What remains is the part sessions do not answer: the cross-origin check,
// which still applies to the routes that reach the panel without a session,
// and the source gate on the mailpit webhook, which is POSTed by a container
// that holds no cookie.
func withRemoteControlGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. CORS preflight: pass through. Browsers don't include the
		// Authorization header on preflight, so requiring auth here would
		// break every cross-origin request from a configured client.
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// 1b. Cross-origin (CSRF) gate. A state-changing request must prove it
		// came from servlo's own dashboard rather than a malicious page open in
		// the developer's browser. This is the one check that also applies to
		// loopback, because the RCE vector is exactly a local browser POSTing
		// to 127.0.0.1:7073/api/sites/<d>/<action>. Exempt endpoints are reached
		// by non-browser clients that have their own source protection.
		if unsafeMethod(r.Method) && !csrfExemptPath(r.URL.Path) && !passesCSRF(r) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Forbidden — cross-origin request blocked. Use the Servlo dashboard itself.", http.StatusForbidden)
			return
		}

		// 2. The remote-setup bootstrap endpoint has its own gate (token,
		// RFC 1918 source IP, brute-force lockout). It must remain reachable
		// from a remote laptop *before* the user has set up dashboard auth.
		if r.URL.Path == "/api/remote-setup" {
			next.ServeHTTP(w, r)
			return
		}

		// 2b. Mailpit's webhook is POSTed from inside the mailpit container
		// to host.containers.internal:7073. pasta (Linux) and gvproxy /
		// vmnet (macOS) source-NAT that to one of the host's own interface
		// IPs, so we accept any caller whose source IP belongs to the
		// host. A LAN attacker arrives from a different IP and is rejected,
		// closing the "anyone on the WiFi can spam fake mail pushes" vector.
		if r.URL.Path == "/api/webhooks/mailpit" {
			if !fromHost(r) {
				w.Header().Set("Cache-Control", "no-store")
				http.Error(w, "Forbidden — this webhook is only accepted from the servlo host.", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleAccessMode serves /api/access-mode. It reports whether LAN exposure is
// enabled, which is a property of the machine rather than of the caller.
func handleAccessMode(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.LoadGlobal()
	lanExposed := cfg != nil && cfg.LAN.Exposed
	writeJSON(w, map[string]any{"lan_exposed": lanExposed})
}

// handleLANStatus serves /api/lan/status.
//
//	GET                               → { exposed, lan_ip }
//	POST { action: "expose" }         → exposes sites, DNS, and dashboard bind
//	POST { action: "unexpose" }       → returns every endpoint to loopback
//
// Databases and caches are not part of this: they bind to the container
// network and nothing publishes them (CLAUDE.md 3.7), so there is no action
// here that could.
func handleLANStatus(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, _ := config.LoadGlobal()
		exposed := false
		if cfg != nil {
			exposed = cfg.LAN.Exposed
		}
		lanIP := ""
		if exposed {
			lanIP = uiPrimaryLANIP()
		}
		writeJSON(w, map[string]any{
			"exposed": exposed,
			"lan_ip":  lanIP,
		})
		return

	case http.MethodPost:
		var body struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch body.Action {
		case "expose", "unexpose":
		default:
			http.Error(w, "unknown action — expected 'expose' or 'unexpose'", http.StatusBadRequest)
			return
		}

		// Stream NDJSON progress so the dashboard can render per-step
		// feedback instead of a single opaque spinner. Each line is a
		// JSON object: {step, status} for in-flight steps and a final
		// {result, exposed, lan_ip, error} envelope when the toggle
		// completes (or errors).
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeLine := func(payload map[string]any) {
			data, _ := json.Marshal(payload)
			_, _ = w.Write(append(data, '\n'))
			if flusher != nil {
				flusher.Flush()
			}
		}
		progress := func(step string) {
			writeLine(map[string]any{"step": step})
		}
		switch body.Action {
		case "expose":
			lanIP, err := servlocli.EnableLANExposure(progress)
			if err != nil {
				writeLine(map[string]any{"result": "error", "error": err.Error()})
				return
			}
			writeLine(map[string]any{
				"result":  "ok",
				"exposed": true,
				"lan_ip":  lanIP,
			})
			return
		case "unexpose":
			if err := servlocli.DisableLANExposure(progress); err != nil {
				writeLine(map[string]any{"result": "error", "error": err.Error()})
				return
			}
			writeLine(map[string]any{
				"result":  "ok",
				"exposed": false,
				"lan_ip":  "",
			})
			return
		}

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

// uiPrimaryLANIP duplicates the dial-trick from cli/dns.go because importing
// cli from ui would risk a cycle. Cheap.
func uiPrimaryLANIP() string {
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

// handleRemoteControl serves /api/remote-control. The middleware already
// gates this endpoint to loopback (because writing the password from a
// browser over HTTP would otherwise expose it to the network), so we don't
// need a second source-IP check here.
//
//	GET                                  → { enabled, username }
//	POST { action: "enable", username, password } → enables, persists hash
//	POST { action: "disable" }           → clears credentials
func handleRemoteControl(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := config.LoadGlobal()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"enabled":  cfg.UI.PasswordHash != "",
			"username": cfg.UI.Username,
		})
		return

	case http.MethodPost:
		var body struct {
			Action   string `json:"action"`
			Username string `json:"username"`
			Password string `json:"password"`
			Enabled  bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		cfg, err := config.LoadGlobal()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		switch body.Action {
		case "enable":
			// In disabled-DNS mode the dashboard chains "set credentials"
			// with "flip lan:expose" into a single user action because the
			// dashboard is effectively the only thing LAN exposure unlocks
			// (a remote device has no way to resolve a loopback-only name). So we
			// only require lan:expose to be on first when DNS is enabled.
			if !cfg.LAN.Exposed && cfg.DNS.Enabled {
				http.Error(w, "LAN exposure is off — run `servlo lan:expose` first. Dashboard credentials are only meaningful while the dashboard is reachable from other devices.", http.StatusBadRequest)
				return
			}
			if body.Username == "" || body.Password == "" {
				http.Error(w, "username and password are required", http.StatusBadRequest)
				return
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
			if err != nil {
				http.Error(w, "hashing password: "+err.Error(), http.StatusInternalServerError)
				return
			}
			cfg.UI.Username = body.Username
			cfg.UI.PasswordHash = string(hash)
			if err := config.SaveGlobal(cfg); err != nil {
				http.Error(w, "saving config: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"ok": true, "enabled": true, "username": body.Username})
			return

		case "disable":
			cfg.UI.Username = ""
			cfg.UI.PasswordHash = ""
			if err := config.SaveGlobal(cfg); err != nil {
				http.Error(w, "saving config: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"ok": true, "enabled": false})
			return

		default:
			http.Error(w, "unknown action — expected 'enable' or 'disable'", http.StatusBadRequest)
			return
		}

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
// Unix-socket requests and requests carrying the private nginx trust token are
// authoritative. A direct TCP request qualifies when its peer is loopback and
// it carries no forwarding headers, which rejects reverse proxies such as
// Tailscale Serve that connect from 127.0.0.1 for a remote browser.
//
// The Host header is deliberately not part of this: a local browser may reach
// the dashboard by the machine's own hostname (Debian and Ubuntu map it to
// 127.0.1.1) or any /etc/hosts alias, and locking those out would leave the
// local user with no way in.

func hasValidTrustToken(r *http.Request) bool {
	claimed := r.Header.Get("X-Servlo-Trust")
	if claimed == "" {
		return false
	}
	token, err := nginx.LoadOrGenerateTrustToken()
	return err == nil && token != "" &&
		subtle.ConstantTimeCompare([]byte(claimed), []byte(token)) == 1
}

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
	if hasValidTrustToken(r) {
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

// isLoopbackRequest reports whether r originates from the local host. Three
// paths qualify:
//
//  1. The connection arrived over the unix socket listener. Only host
//     processes with filesystem access to the socket can connect, so this is
//     at least as trusted as TCP loopback. The servlo.localhost nginx vhost
//     reaches servlo-panel via this path.
//  2. The TCP peer is a loopback IP (127.x, ::1). This catches direct visits
//     to http://localhost:7073 / http://127.0.0.1:7073.
//  3. The request carries an X-Servlo-Trust header whose value matches the
//     per-install token. Kept for backward compatibility with old vhosts
//     that may still inject the header; new installs use the unix socket.
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
	return hasValidTrustToken(r)
}
