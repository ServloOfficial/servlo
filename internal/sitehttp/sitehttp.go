// Package sitehttp makes requests to a site this server is hosting.
//
// A site's domain does not resolve to this server until the operator repoints
// its DNS, and servlo's own documented order puts that after the site exists:
// add the site, point the domain here, then press Get SSL. So anything servlo
// asks a site it hosts cannot be sent to whatever the domain resolves to at the
// time. Before the repoint that address is the operator's previous host, a
// registrar's parking page, or nothing at all, and servlo would be talking to a
// stranger about a site that is sitting on this machine.
//
// It matters most for the one-click app install, which posts a freshly
// generated admin password and the operator's email address into an
// application's own setup form. Sent to the public address, that request is a
// cleartext credential handed to whoever currently answers for the domain, and
// the install then fails on this server because the reply came from a site that
// is not the one servlo built.
//
// So the connection is dialled at this server's own nginx while the URL keeps
// the site's domain, which is what nginx matches server_name against and what
// TLS presents as SNI. Only the address is replaced. internal/uptime does the
// same thing for its own health checks, with its own timeout and redirect
// policy.
package sitehttp

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
)

// loopback is where the connection is actually made, whatever the URL says.
const loopback = "127.0.0.1"

// PortPair is where nginx is listening. Not always 80 and 443: a server that
// took the nftables fallback rather than the unprivileged-port sysctl has nginx
// on 8080 and 8443, and a request aimed at 80 there reaches nothing.
type PortPair struct{ HTTP, HTTPS int }

// Ports reads where nginx actually listens, falling back to the defaults when
// the configuration cannot be read.
func Ports() PortPair {
	p := PortPair{HTTP: 80, HTTPS: 443}
	cfg, err := config.LoadGlobal()
	if err != nil {
		return p
	}
	if cfg.Nginx.HTTPPort != 0 {
		p.HTTP = cfg.Nginx.HTTPPort
	}
	if cfg.Nginx.HTTPSPort != 0 {
		p.HTTPS = cfg.Nginx.HTTPSPort
	}
	return p
}

// URL is the base address servlo uses to talk to a site on this server over
// HTTP. The domain stays in it so nginx picks the right server block; the port
// is the one nginx is actually on.
func URL(domain string) string {
	return "http://" + net.JoinHostPort(domain, strconv.Itoa(Ports().HTTP))
}

// dialTarget is the seam. A test points it at its own server and everything
// above it, including the Host rewrite, stays the real thing.
var dialTarget = func(port string) string { return net.JoinHostPort(loopback, port) }

// Client returns an HTTP client that reaches this server's nginx for every
// request, whatever the URL's host resolves to elsewhere.
func Client(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	return &http.Client{
		Timeout: timeout,
		Transport: hostRewrite{&http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				_, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				return dialer.DialContext(ctx, network, dialTarget(port))
			},
			TLSHandshakeTimeout: timeout,
		}},
	}
}

// hostRewrite drops the port from the Host header.
//
// The URL has to carry nginx's real port to reach it, but the port a site is
// published on is 80 under both of servlo's strategies: the sysctl one puts
// nginx there directly, and the nftables one redirects 80 to 8080 in front of
// it. An application that builds its own URLs from the Host header it was
// installed through would otherwise record the internal port and hand every
// visitor a link to it.
type hostRewrite struct{ http.RoundTripper }

func (h hostRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if bare := hostOf(req); bare != "" {
		req = req.Clone(req.Context())
		req.Host = bare
	}
	return h.RoundTripper.RoundTrip(req)
}

// hostOf is the Host the request would be sent with, minus its port, and empty
// when there is no port to remove. Requests are built with net/http's own
// constructor, which fills Host in from the URL, so this reads whichever of the
// two the caller set.
func hostOf(req *http.Request) string {
	h := req.Host
	if h == "" {
		h = req.URL.Host
	}
	name, _, err := net.SplitHostPort(h)
	if err != nil || name == "" {
		return ""
	}
	return name
}
