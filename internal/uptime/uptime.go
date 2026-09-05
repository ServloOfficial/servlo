// Package uptime asks a site whether it is answering.
//
// The path it asks for is the framework's own, declared in its definition as
// deploy.health, so a framework that ships a real health endpoint gets checked
// against it and one that does not gets checked against its home page. Servlo
// knows no framework's name here (CLAUDE.md §2), only that a definition names a
// path or does not.
//
// The request is made over the loopback with the site's own hostname in the
// URL, so it goes through nginx, the site's vhost and its PHP-FPM pool exactly
// as a visitor's would, while never leaving the machine. Be clear about what
// that does and does not prove: it proves the stack servlo owns is serving, and
// it says nothing about DNS, a cloud firewall, or the network between here and
// the visitor. Those are the things a check running on this server cannot see,
// and pretending otherwise would be worse than saying so.
package uptime

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
)

// Timeout bounds one check. Short: a site that takes longer than this to answer
// its own health path is, for the purpose an operator cares about, down.
const Timeout = 10 * time.Second

// loopback is what the request is actually dialled at, whatever the URL says.
const loopback = "127.0.0.1"

// Result is what one check found.
type Result struct {
	Site string `json:"site"`
	// URL is what was asked for, so a surprising result can be reproduced by
	// hand rather than guessed at.
	URL string `json:"url"`
	// Up is the answer.
	Up bool `json:"up"`
	// Status is the HTTP status, zero when nothing answered.
	Status int `json:"status,omitempty"`
	// Reason is why it is down, empty when it is up.
	Reason string `json:"reason,omitempty"`
	// Took is how long the answer took, which is the early warning: a site
	// answering in eight seconds is on its way to answering in none.
	Took time.Duration `json:"took"`
}

// dialTarget is where a check is actually dialled, whatever the URL says. It is
// the seam: a test points it at its own server, and everything above it —
// redirect policy, timeouts, status handling — stays the real thing.
var dialTarget = func(port string) string { return net.JoinHostPort(loopback, port) }

func client() *http.Client {
	dialer := &net.Dialer{Timeout: Timeout}
	return &http.Client{
		Transport: &http.Transport{
			// The URL carries the site's hostname, so SNI, the vhost's
			// server_name and certificate verification all see the real domain.
			// Only the address dialled is replaced.
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				_, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				return dialer.DialContext(ctx, network, dialTarget(port))
			},
			TLSHandshakeTimeout: Timeout,
		},
		Timeout: Timeout,
		// Redirects are not followed. A site that answers 301 is serving, which
		// is the question, and following would turn one check into several and
		// let a redirect loop hang it.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// URL is what a check of this site asks for. Separate from Check so the address
// servlo would probe can be shown in the panel and asserted on without making a
// request.
func URL(site *config.Site, fw *config.Framework) string {
	domain := site.PrimaryDomain()
	if domain == "" {
		return ""
	}
	if site.Secured {
		return "https://" + net.JoinHostPort(domain, strconv.Itoa(Ports().HTTPS)) + HealthPath(fw)
	}
	return "http://" + net.JoinHostPort(domain, strconv.Itoa(Ports().HTTP)) + HealthPath(fw)
}

// Check asks one site's health path whether it is answering.
func Check(site *config.Site, fw *config.Framework) Result {
	res := Result{Site: site.Name}
	res.URL = URL(site, fw)
	if res.URL == "" {
		res.Reason = "this site has no domain to ask"
		return res
	}

	req, err := http.NewRequest(http.MethodGet, res.URL, nil)
	if err != nil {
		res.Reason = err.Error()
		return res
	}
	// A monitor that looks like a browser gets logged as traffic and counted in
	// a site's analytics. Naming itself keeps a check out of the numbers.
	req.Header.Set("User-Agent", "servlo-uptime/1")

	started := time.Now()
	resp, err := client().Do(req)
	res.Took = time.Since(started)
	if err != nil {
		res.Reason = downReason(err)
		return res
	}
	defer resp.Body.Close() //nolint:errcheck

	res.Status = resp.StatusCode
	if resp.StatusCode >= 500 {
		// 502 and 503 are the shape of a dead PHP-FPM pool behind a live nginx,
		// which is the commonest way a site goes down without the server going
		// anywhere.
		res.Reason = fmt.Sprintf("the site answered %s", resp.Status)
		return res
	}
	// Anything else the server answered means the stack is serving. A 404 on a
	// health path is a wrong path, not a down site, and reporting it as an
	// outage would have an operator chasing the wrong thing at 3am.
	res.Up = true
	return res
}

// HealthPath is where this framework says to ask. A framework that names none
// is asked for its home page, which is a weaker check than a real health
// endpoint and still catches the failure that matters.
func HealthPath(fw *config.Framework) string {
	if fw != nil {
		if h := fw.HealthPath(); h != "" {
			return h
		}
	}
	return "/"
}

// PortPair is where nginx is listening. Not always 80 and 443: a server that
// took the nftables fallback rather than the unprivileged-port sysctl has nginx
// on 8080 and 8443, and a check aimed at 443 there would report every site down.
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

// downReason turns a transport error into something worth reading, because
// "dial tcp 127.0.0.1:443: connect: connection refused" tells an operator only
// that servlo checked loopback.
func downReason(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Sprintf("the site did not answer within %s", Timeout)
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return "the site's certificate did not verify: " + certErr.Error()
	}
	return "the site could not be reached: " + err.Error()
}
