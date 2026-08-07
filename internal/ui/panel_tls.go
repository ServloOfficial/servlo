package ui

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/realrashid/servlo/internal/certs"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dnscheck"
)

// The panel over TLS.
//
// Upstream served the dashboard over plain HTTP, which is right for a laptop
// reaching 127.0.0.1 and wrong for a droplet: on a server the wire between the
// operator and the panel is the internet, and the first thing they do over it
// is type a password. So the listener speaks TLS, with a self-signed
// certificate until a domain points at the panel and a real one replaces it.
//
// It is one port rather than two. A second listener for the redirect would need
// a second port in every firewall rule and every piece of documentation, to
// serve a response whose entire content is "use the other one". Instead the
// first byte of each connection decides: a TLS handshake starts with 0x16, and
// anything else is treated as plain HTTP and answered with a redirect.

// tlsHandshakeRecord is the first byte of every TLS ClientHello: a record of
// type handshake (22). No plausible plain-HTTP request begins with it, since
// every HTTP method starts with an uppercase letter.
const tlsHandshakeRecord = 0x16

// servePanelTLS serves handler over ln, speaking TLS to TLS clients and
// answering plain HTTP with a redirect to the same URL over https.
func servePanelTLS(ln net.Listener, handler http.Handler) error {
	cfg := &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: panelCertificateFor,
	}
	sorted := &protocolSortingListener{Listener: ln, plain: make(chan net.Conn, 16)}

	// The redirect responder is an ordinary HTTP server over the connections
	// the sorter hands it, so it gets request parsing, timeouts and keep-alive
	// handling for free rather than a hand-rolled reply.
	go func() {
		_ = (&http.Server{Handler: http.HandlerFunc(redirectToHTTPS)}).Serve(&channelListener{
			conns: sorted.plain,
			addr:  ln.Addr(),
		})
	}()

	return (&http.Server{Handler: handler, TLSConfig: cfg}).ServeTLS(sorted, "", "")
}

// redirectToHTTPS answers a plain-HTTP request with the same URL over https.
//
// It never reaches the panel's own handler. Whatever the request carried was
// already in the clear, and answering it with real content, or with a cookie,
// would make the mistake worse rather than correct it.
func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	target := "https://" + r.Host + r.URL.RequestURI()
	w.Header().Set("Location", target)
	w.Header().Set("Cache-Control", "no-store")
	// 308 rather than 301: a POST that arrived in the clear should be replayed
	// as a POST, and 301 lets a client turn it into a GET.
	w.WriteHeader(http.StatusPermanentRedirect)
	fmt.Fprintf(w, "The Servlo panel speaks HTTPS. Use %s\n", target)
}

// protocolSortingListener peeks one byte from each accepted connection and
// returns TLS connections to Accept, diverting the rest to plain.
type protocolSortingListener struct {
	net.Listener
	plain chan net.Conn

	closeOnce sync.Once
}

func (l *protocolSortingListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			l.closeOnce.Do(func() { close(l.plain) })
			return nil, err
		}
		peeked := &peekedConn{Conn: conn, reader: bufio.NewReader(conn)}
		first, err := peeked.reader.Peek(1)
		if err != nil {
			// A connection that closed before saying anything is a health
			// probe or a port scan. Dropping it is the whole response.
			_ = conn.Close()
			continue
		}
		if first[0] == tlsHandshakeRecord {
			return peeked, nil
		}
		select {
		case l.plain <- peeked:
		default:
			// The redirect responder is not keeping up, which means something
			// is flooding the port with plain HTTP. Dropping is better than
			// blocking the accept loop and starving real TLS clients.
			_ = conn.Close()
		}
	}
}

// peekedConn restores the byte Peek consumed, so the reader downstream sees the
// connection exactly as it arrived.
type peekedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

// channelListener presents a channel of connections as a net.Listener, so the
// diverted plain-HTTP connections can be served by an ordinary http.Server.
type channelListener struct {
	conns chan net.Conn
	addr  net.Addr
}

func (l *channelListener) Accept() (net.Conn, error) {
	conn, ok := <-l.conns
	if !ok {
		return nil, net.ErrClosed
	}
	return conn, nil
}

// Close is a no-op: the sorting listener owns the socket and closes the channel
// when it stops accepting, which is what ends the redirect server.
func (l *channelListener) Close() error   { return nil }
func (l *channelListener) Addr() net.Addr { return l.addr }

// panelCertificateFor supplies the certificate for each handshake, so a domain
// attached or an address changed while the panel is running takes effect on the
// next connection rather than at the next restart.
func panelCertificateFor(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	domain := PanelDomain()
	if real, err := panelRealCertificate(domain); err == nil {
		return real, nil
	}
	addrs, _ := dnscheck.ServerAddresses(hello.Context())
	return certs.PanelCertificate(addrs, domain)
}

// panelRealCertificate returns the certificate issued for the panel's domain
// through the ordinary ACME flow, once there is one. Until then there is no
// file and the self-signed one stands in.
func panelRealCertificate(domain string) (*tls.Certificate, error) {
	if domain == "" {
		return nil, net.ErrClosed
	}
	certPath, keyPath := certs.SitePaths(domain)
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// PanelDomain is the FQDN the panel answers to, empty until one is attached.
func PanelDomain() string {
	cfg, err := config.LoadGlobal()
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.UI.Domain)
}
