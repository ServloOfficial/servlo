package nginx

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/realrashid/servlo/internal/config"
)

// The panel on a real domain.
//
// Reaching the panel by address over a self-signed certificate is the first
// hour of a droplet's life, not the way to run it. Once a domain points here,
// nginx serves the panel like any site: the same ACME challenge location, the
// same certificate directory, the same TLS defaults, so the certificate arrives
// through the ordinary Get SSL flow rather than a second issuance path that
// would have to be kept working alongside it.
//
// The upstream is servlo-panel's unix socket, which is already bind-mounted
// into the nginx container for the servlo.localhost vhost. That keeps the
// container-to-host hop off the network entirely.

const panelVhostFile = "servlo-panel.conf"

// panelSecurityHeaders are what the panel needs beyond a site's defaults. A
// framed panel is a clickjacked delete button, and the panel has no legitimate
// reason to be embedded by anything.
const panelSecurityHeaders = `    add_header X-Frame-Options "DENY" always;
    add_header Content-Security-Policy "frame-ancestors 'none'" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "same-origin" always;
`

// PanelVhostPath is where the panel's vhost lives.
func PanelVhostPath() string { return filepath.Join(config.NginxConfD(), panelVhostFile) }

// EnsurePanelVhost writes the nginx vhost that serves the panel on domain.
//
// secured says whether a certificate for the domain exists yet. Before it does
// the vhost is port 80 only: naming a certificate file that is not there stops
// nginx from starting, which would take every site on the box down with it.
func EnsurePanelVhost(domain string, secured bool) error {
	if domain == "" {
		return fmt.Errorf("no panel domain to write a vhost for")
	}
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}

	var content string
	if secured {
		content = fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %[1]s;
%[2]s
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name %[1]s;
%[2]s
    ssl_certificate /etc/nginx/certs/%[1]s.crt;
    ssl_certificate_key /etc/nginx/certs/%[1]s.key;
%[3]s%[4]s%[5]s}
`, domain, acmeChallengeLocation, tlsBlockFor(domain), panelSecurityHeaders, panelProxyBlock())
	} else {
		content = fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %[1]s;
%[2]s%[3]s%[4]s}
`, domain, acmeChallengeLocation, panelSecurityHeaders, panelProxyBlock())
	}

	config.GuardRealWrite(PanelVhostPath())
	return os.WriteFile(PanelVhostPath(), []byte(content), 0644)
}

// RemovePanelVhost deletes the panel vhost. An absent one is not an error:
// every caller reaches this to make sure it is gone, not because it knows.
func RemovePanelVhost() error {
	config.GuardRealWrite(PanelVhostPath())
	if err := os.Remove(PanelVhostPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// panelProxyBlock proxies the whole vhost to servlo-panel over the unix socket
// the container already has mounted, websockets included: the dashboard streams
// deploy output and log tails over one.
func panelProxyBlock() string {
	return fmt.Sprintf(`    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    # Says "this arrived from the public internet, not the local dashboard".
    # servlo-panel treats its unix socket as local control, which is right for
    # the servlo.localhost vhost and would otherwise be catastrophically wrong
    # here. proxy_set_header overwrites anything the client sent, so it cannot
    # be stripped from outside.
    proxy_set_header X-Servlo-Public-Panel 1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_read_timeout 3600s;

    location / {
        proxy_pass http://unix:%s:$request_uri;
    }
`, config.UISocketPath())
}
