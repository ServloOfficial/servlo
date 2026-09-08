package nginx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/envfile"
	"github.com/ServloOfficial/servlo/internal/podman"
)

// detectSiteProxy checks the site's framework definition for a worker with a
// proxy configuration. Returns the proxy path and port if found. The path is
// regexp.QuoteMeta-escaped: the vhost templates interpolate it into an nginx
// regex location (`location ~ ^{{.ProxyPath}}(/|$)`) so the anchoring only
// matches that path and its subpaths, not an unrelated one sharing the same
// prefix. A framework-declared path is realistically literal but can contain
// regex metacharacters a template author never intended as regex — "/socket.io"
// being the common one, where an unescaped "." would also match "/socketXio".
// The path is normalised first, since every way of writing one that reads
// naturally has to anchor the same: a trailing slash is trimmed (`^/app/(/|$)`
// would match the literal "/app/" and nothing else, not "/app" and nothing
// under it) and a missing leading slash is added (`^app(/|$)` can never match a
// URI, which always starts with "/"). A proxy declaring no path at all names
// nothing to proxy, and is reported as no proxy rather than capturing the site.
func detectSiteProxy(site config.Site) (path string, port int, ok bool) {
	fw, fwOK := config.GetFrameworkForDir(site.Framework, site.Path)
	if !fwOK {
		return "", 0, false
	}
	proxy, _ := fw.DetectProxy(site.Path)
	if proxy == nil {
		return "", 0, false
	}
	proxyPort := proxy.DefaultPort
	if proxyPort == 0 {
		proxyPort = 8080
	}
	if proxy.PortEnvKey != "" {
		if v := envfile.ReadKey(filepath.Join(site.Path, ".env"), proxy.PortEnvKey); v != "" {
			if p, err := strconv.Atoi(v); err == nil && p > 0 {
				proxyPort = p
			}
		}
	}
	if proxy.Path == "" {
		return "", 0, false
	}
	// "/" trims to empty, which renders `^(/|$)`: the root and everything under
	// it, what a root-mounted worker means. Restoring it to "/" instead would
	// render `^/(/|$)`, matching "/" and "//" and nothing else.
	proxyPath := strings.TrimSuffix(proxy.Path, "/")
	if proxyPath != "" && !strings.HasPrefix(proxyPath, "/") {
		proxyPath = "/" + proxyPath
	}
	return regexp.QuoteMeta(proxyPath), proxyPort, true
}

// detectSiteDevServer returns the prefix and host port for a site whose
// framework declares a dev server, once that server has been pinned to a port.
// An unpinned site has never started one, so nothing is proxied.
func detectSiteDevServer(site config.Site) (base string, port int) {
	if site.DevServerPort == 0 {
		return "", 0
	}
	tool := config.DevServerToolInstalled(site.Path)
	if tool == nil {
		return "", 0
	}
	return tool.Base, site.DevServerPort
}

type nginxConfData struct {
	Resolver        string
	AccessLogTarget string
}

// VhostData is the data passed to vhost templates.
type VhostData struct {
	Domain          string // primary domain (used for config file naming)
	ServerNames     string // space-separated list of all domains for server_name directive
	Path            string
	PHPVersion      string
	PHPVersionShort string
	// FPMContainer is the container nginx fastcgi's to: the shared
	// servlo-php<ver>-fpm, or a per-site container for custom-FPM sites.
	FPMContainer string
	// FPMSocket is the site's own pool socket, set only once that pool exists.
	// Empty falls back to FPMContainer, which is what keeps a site that has not
	// been given a pool yet serving instead of answering 502.
	FPMSocket       string
	CertDomain      string // domain whose cert files to use (defaults to Domain)
	PublicDir       string // document root subdirectory, e.g. "public", "web", "."
	Proxy           bool   // true when the site has a worker with WebSocket/HTTP proxy config
	ProxyPath       string // URL path for the proxy (e.g. "/app")
	ProxyPort       int    // port the worker listens on inside the PHP-FPM container
	CustomContainer string // container name for custom container sites (e.g. "servlo-custom-nestapp")
	CustomPort      int    // port the app listens on inside the custom container
	// DevServerBase is the URL prefix a host dev server serves everything
	// under, so one location covers its assets and its hot-reload socket.
	DevServerBase string
	DevServerPort int
	UpstreamHost  string // host-proxy upstream address (e.g. "host.containers.internal")
	UpstreamPort  int    // host-proxy upstream port (the dev server's host port)
	BackendSSL    bool   // proxy to the backend via HTTPS (app serves TLS on its own port)
	// ServloSite / ServloBranch surface the site name and the branch to PHP via
	// fastcgi_param so events carry stable identifiers instead of a guess from
	// DOCUMENT_ROOT.
	ServloSite   string
	ServloBranch string
	// RequestTimeout is the nginx request timeout in seconds rendered into the
	// fastcgi_*_timeout / proxy_*_timeout directives. Resolved per site by
	// siteRequestTimeout: the site's own max execution time first, then the
	// project .servlo.yaml, then global config, then 60s. It is nginx's half of
	// the max-execution-time pair, whose PHP half is written into the pool.
	RequestTimeout int
	// MaxUploadMB is the site's upload ceiling, nginx's half of the pair whose
	// PHP half (upload_max_filesize and post_max_size) is written into the
	// pool. Zero writes no directive and leaves nginx's own default.
	MaxUploadMB int
	// StaticCacheDays and ResponseHeaders are the site's own nginx settings,
	// rendered by StaticCache and SiteHeaders. Zero and empty write nothing.
	StaticCacheDays int
	ResponseHeaders []config.ResponseHeader
	// CanonicalHost is which of the site's two www forms is the real one, and
	// the other is permanently redirected to it. Empty leaves both serving.
	CanonicalHost string
	// RedirectTo, RedirectPermanent and Redirects are the site's redirects,
	// rendered by WholeDomainRedirect and URLRedirects.
	RedirectTo        string
	RedirectPermanent bool
	Redirects         []config.Redirect
	// Staging is set for a staging site and carries what makes it one: it is
	// not indexed, and it is behind a password. Nil for an ordinary site.
	Staging *config.SiteStaging
	// FrameworkNginx is the framework definition's nginx block, already
	// placeholder-expanded and indented. Rendered ahead of the generic
	// locations so a framework can claim paths they would otherwise swallow.
	FrameworkNginx string
}

// Root is the document root as the templates render it, quoted so a path with a
// space stays a single nginx token. Every generator that fills a VhostData goes
// through it, so the plain and SSL vhosts are both covered.
func (d VhostData) Root() string {
	return nginxQuote(d.Path + "/" + d.PublicDir)
}

// ACMEChallenge is the HTTP-01 location block. A method rather than a field so
// no generator can forget to set it: every site needs a certificate eventually,
// and a vhost that silently omits this fails its first renewal instead of its
// first request.
func (d VhostData) ACMEChallenge() string {
	return fmt.Sprintf(acmeChallengeLocation, d.stagingChallengeExemption())
}

// UploadLimit is nginx's half of the max-upload-size pair, rendered as a whole
// directive so a template cannot spell it or place it differently between the
// plain and SSL vhosts. Empty when the site sets no limit, because a directive
// restating nginx's own default would stop the global config from moving it.
//
// A method for the same reason as ACMEChallenge: every vhost gets it, and one
// that quietly omits it is a site whose upload is refused by nginx before PHP
// ever sees the request, with the operator looking at a PHP setting that says
// the upload is allowed.
func (d VhostData) UploadLimit() string {
	if d.MaxUploadMB <= 0 {
		return ""
	}
	return fmt.Sprintf("    client_max_body_size %dm;\n", d.MaxUploadMB)
}

// HSTS is the TLS configuration a secured vhost carries: protocol and cipher
// defaults, the Strict-Transport-Security header, and OCSP stapling where the
// certificate supports it. A method for the same reason as ACMEChallenge:
// every secured site gets it, and one that quietly does not is a site whose
// first request of every session is interceptable.
//
// The name is kept from when this was only the header, because the templates
// substitute it by name and renaming it there would be churn for nothing.
func (d VhostData) HSTS() string { return tlsBlockFor(d.CertDomain) }

// resolveFrameworkNginx returns the site framework's nginx block, expanded and
// indented for splicing into the server block. Empty when the framework declares
// none, when the snippet is unbalanced, or when a substituted value carries
// nginx syntax of its own.
func resolveFrameworkNginx(site config.Site, publicDir string, up Upstream) string {
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok || fw.Nginx == nil {
		return ""
	}
	var warn bytes.Buffer
	block := frameworkNginxBlock(&warn, site.Framework, site.PrimaryDomain(), fw.Nginx.Snippet, site.Path, publicDir, up)
	emitOnce(os.Stdout, warn.String())
	return block
}

var (
	frameworkNginxWarnMu   sync.Mutex
	frameworkNginxWarnSeen = map[string]bool{}
)

// emitOnce writes msg to w only the first time this process sees it. A dropped
// snippet then surfaces on `servlo link` without the watcher repeating it on every
// vhost regeneration, and the http/ssl pair for one site collapses to one line.
func emitOnce(w io.Writer, msg string) {
	if msg == "" {
		return
	}
	frameworkNginxWarnMu.Lock()
	defer frameworkNginxWarnMu.Unlock()
	if frameworkNginxWarnSeen[msg] {
		return
	}
	frameworkNginxWarnSeen[msg] = true
	fmt.Fprint(w, msg)
}

// frameworkNginxBlock validates and expands a framework snippet into an indented
// server-block fragment, "" when it declares none. A drop writes a warning to w
// rather than vanishing silently (the caller rate-limits it, see emitOnce).
func frameworkNginxBlock(w io.Writer, framework, domain, snippet, sitePath, publicDir string, up Upstream) string {
	if strings.TrimSpace(snippet) == "" {
		return ""
	}
	if err := config.ValidateNginxSnippet(snippet); err != nil {
		fmt.Fprintf(w, "[WARN] dropping %s nginx config for %s: %v\n", framework, domain, err)
		return ""
	}
	expanded, err := expandNginxSnippet(snippet, sitePath, publicDir, up)
	if err != nil {
		fmt.Fprintf(w, "[WARN] dropping %s nginx config for %s: %v\n", framework, domain, err)
		return ""
	}
	return indentBlock(strings.TrimRight(expanded, "\n"), "    ")
}

// nginxValueForbidden are the characters that let a substituted value break out
// of the directive it lands in: braces open or close blocks, `;` ends a
// directive, `#` comments out the rest of the line, newlines do both.
const nginxValueForbidden = "{};#\n\r\x00"

// validate refuses any value that could break out of the directive it lands in.
// A project's .servlo.yaml supplies the domains, the public dir and the path,
// any of which may legally carry `;` and braces. Checked here rather than per
// field so a field added to the struct
// later is covered without anyone remembering. Ints and bools cannot carry
// syntax, so only the strings are examined.
//
// Paths reach the template through nginxQuote, which handles whitespace but not
// these; a quoted token still ends at an unescaped `"`, and `;` inside one is
// only safe while the quoting holds.
func (d VhostData) validate() error {
	// Substituted bare, so anything that ends a directive or opens a block is
	// syntax the value must not carry.
	for name, v := range map[string]string{
		"server names":     d.ServerNames,
		"domain":           d.Domain,
		"cert domain":      d.CertDomain,
		"proxy path":       d.ProxyPath,
		"PHP version":      d.PHPVersion,
		"FPM container":    d.FPMContainer,
		"custom container": d.CustomContainer,
		"upstream host":    d.UpstreamHost,
		"site":             d.ServloSite,
		"branch":           d.ServloBranch,
	} {
		if i := strings.IndexAny(v, nginxValueForbidden); i >= 0 {
			return fmt.Errorf("nginx %s %q contains %q, which would end the directive it lands in", name, v, string(v[i]))
		}
	}
	// The paths reach the templates only through Root(), which quotes them, so
	// `;` and `#` are literal there and a directory legitimately containing one
	// still serves. A line break is the one thing quoting does not contain.
	for name, v := range map[string]string{"path": d.Path, "public dir": d.PublicDir} {
		if i := strings.IndexAny(v, "\n\r\x00"); i >= 0 {
			return fmt.Errorf("nginx %s %q contains %q, which would break out of its line", name, v, string(v[i]))
		}
	}
	return nil
}

// renderVhost validates the substituted values and renders the template. Every
// vhost goes through here, so nothing reaches conf.d without being checked.
func renderVhost(tmpl *template.Template, data VhostData) ([]byte, error) {
	if err := data.validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// nginxQuote renders a filesystem path as a quoted nginx token. nginx splits a
// directive on whitespace, so a project under a path with a space would give
// root three arguments and nginx rejects the whole config, taking every other
// site down with it. Backslash-escaping the spaces parses but does not work:
// nginx keeps the backslash in the value and realpath() then fails on it.
func nginxQuote(p string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(p) + `"`
}

// Variables carrying the site paths for framework snippets to interpolate.
const (
	nginxRootVar   = "${servlo_root}"
	nginxPublicVar = "${servlo_public}"
)

// nginxPathVars declares the variables a framework snippet interpolates. A
// snippet may use a path mid-token, as Magento's does with {{public}}/static/,
// and a quoted token cannot be glued to anything, so the path reaches the
// snippet as a variable: nginx resolves it after tokenizing, spaces and all.
func nginxPathVars(sitePath, docRoot string) string {
	return fmt.Sprintf("set $servlo_root %s;\nset $servlo_public %s;\n\n",
		nginxQuote(sitePath), nginxQuote(docRoot))
}

// expandNginxSnippet substitutes the placeholders a framework snippet may use.
// Plain string replacement, not text/template: the snippet is data rendered into
// a template, so its braces must never be evaluated as template actions. Values
// are rejected rather than escaped, since nginx has no general escape for them
// and every legitimate value here is a path or a container name.
func expandNginxSnippet(snippet, sitePath, publicDir string, up Upstream) (string, error) {
	docRoot := sitePath
	if publicDir != "" && publicDir != "." {
		docRoot = filepath.Join(sitePath, publicDir)
	}
	for _, v := range []string{sitePath, docRoot, up.Container, up.Socket} {
		if i := strings.IndexAny(v, nginxValueForbidden); i >= 0 {
			return "", fmt.Errorf("nginx value %q contains %q", v, v[i])
		}
	}
	out := strings.NewReplacer(
		"{{root}}", nginxRootVar,
		"{{public}}", nginxPublicVar,
		"{{fpm}}", up.Container,
		"{{fastcgi_pass}}", fastcgiPassBlock(up),
	).Replace(snippet)
	// A misspelled placeholder has balanced braces, so it survives validation and
	// would reach nginx verbatim, breaking the config for every site.
	if strings.Contains(out, "{{") {
		return "", fmt.Errorf("nginx snippet has an unknown {{placeholder}}")
	}
	if strings.Contains(out, nginxRootVar) || strings.Contains(out, nginxPublicVar) {
		out = nginxPathVars(sitePath, docRoot) + out
	}
	return out, nil
}

// indentBlock prefixes every non-blank line with indent.
func indentBlock(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = indent + l
		}
	}
	return strings.Join(lines, "\n")
}

// resolveRequestTimeout returns the effective request timeout in seconds for
// the site at sitePath: project .servlo.yaml wins, then global config, then 60s.
// An empty sitePath skips the project lookup (site-less proxy vhosts).
func resolveRequestTimeout(sitePath string) int {
	if sitePath != "" {
		if pc, err := config.LoadProjectConfig(sitePath); err == nil && pc.RequestTimeout > 0 {
			return pc.RequestTimeout
		}
	}
	gc, err := config.LoadGlobal()
	if err != nil {
		return config.DefaultRequestTimeout
	}
	return gc.RequestTimeoutSeconds()
}

// siteRequestTimeout is nginx's half of the max-execution-time pair. The site's
// own setting is the more specific one, so it wins over whatever the project
// and global resolution produced; a site that sets none keeps that.
//
// The two have to agree or the pair is only half exposed: PHP set to 600 with
// nginx still at 60 means the request is cut off at 60, and the operator is
// looking at a PHP setting that says otherwise.
func siteRequestTimeout(site config.Site, resolved int) int {
	if site.MaxExecutionSeconds > 0 {
		return site.MaxExecutionSeconds
	}
	return resolved
}

// phpShort converts "8.4" → "84".
func phpShort(version string) string {
	return strings.ReplaceAll(version, ".", "")
}

// resolvePublicDir returns the document root subdirectory for a site.
// site.PublicDir wins (set from .servlo.yaml's public_dir, or from autodetect
// when no framework matched), then the framework definition's PublicDir, then
// "public" as the final fallback. Each candidate runs through ValidatePublicDir
// so a hostile .servlo.yaml can't pivot the nginx root out of the project.
func resolvePublicDir(site config.Site) string {
	if site.PublicDir != "" {
		if err := config.ValidatePublicDir(site.PublicDir); err == nil {
			return site.PublicDir
		}
	}
	if fw, ok := config.GetFrameworkForDir(site.Framework, site.Path); ok && fw.PublicDir != "" {
		if err := config.ValidatePublicDir(fw.PublicDir); err == nil {
			return fw.PublicDir
		}
	}
	return "public"
}

// serverNames is the site's server_name: the domains it was given, and nothing
// else.
//
// It used to add a *.domain wildcard for each, so an unregistered subdomain was
// served by its parent. That is a convenience worth having on a laptop and
// wrong on a server. The parent's certificate does not name the subdomain, so a
// visitor reaching it over HTTPS meets a browser security warning served by a
// site nobody meant to put there; and unlinking a subdomain that did have a
// site of its own silently handed its traffic back to the parent rather than
// answering "not found".
//
// A site that genuinely wants every subdomain adds the wildcard as a domain of
// its own, which is passed through here untouched. That needs a wildcard
// certificate, which servlo can only issue over DNS-01, so it is a decision the
// operator makes once rather than one applied to every site by default.
func serverNames(domains []string) string {
	return strings.Join(domains, " ")
}

// GenerateVhost renders the HTTP vhost template and writes it to conf.d.
func GenerateVhost(site config.Site, phpVersion string) error {
	tmplData, err := GetTemplate("vhost.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost").Parse(string(tmplData))
	if err != nil {
		return err
	}

	publicDir := resolvePublicDir(site)
	serverNames := serverNames(site.Domains)

	proxyPath, proxyPort, hasProxy := detectSiteProxy(site)
	devBase, devPort := detectSiteDevServer(site)
	fpmContainer := podman.FPMContainerName(site, phpVersion)
	upstream := FPMUpstream(config.FPMPoolDir(fpmContainer), config.FPMSocketDir(), site.Name, fpmContainer)
	data := VhostData{
		Domain:            site.PrimaryDomain(),
		ServerNames:       serverNames,
		Path:              site.Path,
		PHPVersion:        phpVersion,
		PHPVersionShort:   phpShort(phpVersion),
		FPMContainer:      fpmContainer,
		FPMSocket:         upstream.Socket,
		PublicDir:         publicDir,
		Proxy:             hasProxy,
		ProxyPath:         proxyPath,
		ProxyPort:         proxyPort,
		UpstreamHost:      hostProxyUpstream(),
		DevServerBase:     devBase,
		DevServerPort:     devPort,
		ServloSite:        site.Name,
		RequestTimeout:    siteRequestTimeout(site, resolveRequestTimeout(site.Path)),
		MaxUploadMB:       site.MaxUploadMB,
		StaticCacheDays:   site.StaticCacheDays,
		ResponseHeaders:   site.ResponseHeaders,
		CanonicalHost:     site.CanonicalHost,
		Staging:           site.Staging,
		RedirectTo:        site.RedirectTo,
		RedirectPermanent: site.RedirectPermanent,
		Redirects:         site.Redirects,
		FrameworkNginx:    resolveFrameworkNginx(site, publicDir, upstream),
	}

	rendered, err := renderVhost(tmpl, data)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	config.GuardRealWrite(confPath)
	return commitVhost(confPath, rendered)
}

// GenerateSSLVhost renders the SSL vhost template and writes it to conf.d.
func GenerateSSLVhost(site config.Site, phpVersion string) error {
	tmplData, err := GetTemplate("vhost-ssl.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost-ssl").Parse(string(tmplData))
	if err != nil {
		return err
	}

	publicDir := resolvePublicDir(site)
	serverNames := serverNames(site.Domains)

	proxyPath, proxyPort, hasProxy := detectSiteProxy(site)
	devBase, devPort := detectSiteDevServer(site)
	fpmContainer := podman.FPMContainerName(site, phpVersion)
	upstream := FPMUpstream(config.FPMPoolDir(fpmContainer), config.FPMSocketDir(), site.Name, fpmContainer)
	data := VhostData{
		Domain:            site.PrimaryDomain(),
		ServerNames:       serverNames,
		Path:              site.Path,
		PHPVersion:        phpVersion,
		PHPVersionShort:   phpShort(phpVersion),
		FPMContainer:      fpmContainer,
		FPMSocket:         upstream.Socket,
		CertDomain:        site.PrimaryDomain(),
		PublicDir:         publicDir,
		Proxy:             hasProxy,
		ProxyPath:         proxyPath,
		ProxyPort:         proxyPort,
		UpstreamHost:      hostProxyUpstream(),
		DevServerBase:     devBase,
		DevServerPort:     devPort,
		ServloSite:        site.Name,
		RequestTimeout:    siteRequestTimeout(site, resolveRequestTimeout(site.Path)),
		MaxUploadMB:       site.MaxUploadMB,
		StaticCacheDays:   site.StaticCacheDays,
		ResponseHeaders:   site.ResponseHeaders,
		CanonicalHost:     site.CanonicalHost,
		Staging:           site.Staging,
		RedirectTo:        site.RedirectTo,
		RedirectPermanent: site.RedirectPermanent,
		Redirects:         site.Redirects,
		FrameworkNginx:    resolveFrameworkNginx(site, publicDir, upstream),
	}

	rendered, err := renderVhost(tmpl, data)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
	config.GuardRealWrite(confPath)
	return commitVhost(confPath, rendered)
}

// GenerateFrankenPHPVhost renders the HTTP vhost template for a FrankenPHP
// site. Nginx reverse-proxies to the per-site servlo-fp-<name>:8000 container
// using the shared custom-container template.
func GenerateFrankenPHPVhost(site config.Site) error {
	tmplData, err := GetTemplate("vhost-custom.conf.tmpl")
	if err != nil {
		return err
	}
	tmpl, err := template.New("vhost-custom").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:            site.PrimaryDomain(),
		ServerNames:       serverNames(site.Domains),
		CustomContainer:   podman.FrankenPHPContainerName(site.Name),
		CustomPort:        podman.FrankenPHPPort,
		RequestTimeout:    siteRequestTimeout(site, resolveRequestTimeout(site.Path)),
		MaxUploadMB:       site.MaxUploadMB,
		StaticCacheDays:   site.StaticCacheDays,
		ResponseHeaders:   site.ResponseHeaders,
		CanonicalHost:     site.CanonicalHost,
		Staging:           site.Staging,
		RedirectTo:        site.RedirectTo,
		RedirectPermanent: site.RedirectPermanent,
		Redirects:         site.Redirects,
	}

	rendered, err := renderVhost(tmpl, data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	config.GuardRealWrite(confPath)
	return commitVhost(confPath, rendered)
}

// GenerateFrankenPHPSSLVhost renders the HTTPS vhost template for a FrankenPHP site.
func GenerateFrankenPHPSSLVhost(site config.Site) error {
	tmplData, err := GetTemplate("vhost-custom-ssl.conf.tmpl")
	if err != nil {
		return err
	}
	tmpl, err := template.New("vhost-custom-ssl").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:            site.PrimaryDomain(),
		ServerNames:       serverNames(site.Domains),
		CertDomain:        site.PrimaryDomain(),
		CustomContainer:   podman.FrankenPHPContainerName(site.Name),
		CustomPort:        podman.FrankenPHPPort,
		RequestTimeout:    siteRequestTimeout(site, resolveRequestTimeout(site.Path)),
		MaxUploadMB:       site.MaxUploadMB,
		StaticCacheDays:   site.StaticCacheDays,
		ResponseHeaders:   site.ResponseHeaders,
		CanonicalHost:     site.CanonicalHost,
		Staging:           site.Staging,
		RedirectTo:        site.RedirectTo,
		RedirectPermanent: site.RedirectPermanent,
		Redirects:         site.Redirects,
	}

	rendered, err := renderVhost(tmpl, data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
	config.GuardRealWrite(confPath)
	return commitVhost(confPath, rendered)
}

// GenerateCustomVhost renders the HTTP vhost template for a custom container
// site and writes it to conf.d. Nginx reverse-proxies to the container instead
// of using fastcgi_pass.
func GenerateCustomVhost(site config.Site) error {
	tmplData, err := GetTemplate("vhost-custom.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost-custom").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:            site.PrimaryDomain(),
		ServerNames:       serverNames(site.Domains),
		CustomContainer:   podman.CustomContainerName(site.Name),
		CustomPort:        site.ContainerPort,
		BackendSSL:        site.ContainerSSL,
		RequestTimeout:    siteRequestTimeout(site, resolveRequestTimeout(site.Path)),
		MaxUploadMB:       site.MaxUploadMB,
		StaticCacheDays:   site.StaticCacheDays,
		ResponseHeaders:   site.ResponseHeaders,
		CanonicalHost:     site.CanonicalHost,
		Staging:           site.Staging,
		RedirectTo:        site.RedirectTo,
		RedirectPermanent: site.RedirectPermanent,
		Redirects:         site.Redirects,
	}

	rendered, err := renderVhost(tmpl, data)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	config.GuardRealWrite(confPath)
	return commitVhost(confPath, rendered)
}

// GenerateCustomSSLVhost renders the SSL vhost template for a custom container
// site and writes it to conf.d.
func GenerateCustomSSLVhost(site config.Site) error {
	tmplData, err := GetTemplate("vhost-custom-ssl.conf.tmpl")
	if err != nil {
		return err
	}

	tmpl, err := template.New("vhost-custom-ssl").Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:            site.PrimaryDomain(),
		ServerNames:       serverNames(site.Domains),
		CertDomain:        site.PrimaryDomain(),
		CustomContainer:   podman.CustomContainerName(site.Name),
		CustomPort:        site.ContainerPort,
		BackendSSL:        site.ContainerSSL,
		RequestTimeout:    siteRequestTimeout(site, resolveRequestTimeout(site.Path)),
		MaxUploadMB:       site.MaxUploadMB,
		StaticCacheDays:   site.StaticCacheDays,
		ResponseHeaders:   site.ResponseHeaders,
		CanonicalHost:     site.CanonicalHost,
		Staging:           site.Staging,
		RedirectTo:        site.RedirectTo,
		RedirectPermanent: site.RedirectPermanent,
		Redirects:         site.Redirects,
	}

	rendered, err := renderVhost(tmpl, data)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
	config.GuardRealWrite(confPath)
	return commitVhost(confPath, rendered)
}

// hostProxyUpstream returns the host address nginx proxies a host-proxy site to.
// macOS resolves host.containers.internal via gvproxy; on Linux we reuse the
// routable gateway IP the probe cached in the hosts file (pure read, no podman).
func hostProxyUpstream() string {
	if ip := podman.ReadHostGatewayFromFile(); ip != "" {
		return ip
	}
	return "host.containers.internal"
}

// GenerateHostProxyVhost renders the HTTP vhost for a host-proxy site and writes
// it to conf.d. Nginx reverse-proxies to a process on the host instead of a
// container.
func GenerateHostProxyVhost(site config.Site) error {
	return generateHostProxyVhost(site, "vhost-hostproxy.conf.tmpl", site.PrimaryDomain()+".conf", false)
}

// GenerateHostProxySSLVhost renders the SSL vhost for a host-proxy site and
// writes it to conf.d/<domain>-ssl.conf.
func GenerateHostProxySSLVhost(site config.Site) error {
	return generateHostProxyVhost(site, "vhost-hostproxy-ssl.conf.tmpl", site.PrimaryDomain()+"-ssl.conf", true)
}

func generateHostProxyVhost(site config.Site, tmplName, confName string, ssl bool) error {
	tmplData, err := GetTemplate(tmplName)
	if err != nil {
		return err
	}
	tmpl, err := template.New(tmplName).Parse(string(tmplData))
	if err != nil {
		return err
	}

	data := VhostData{
		Domain:            site.PrimaryDomain(),
		ServerNames:       serverNames(site.Domains),
		UpstreamHost:      hostProxyUpstream(),
		UpstreamPort:      site.HostPort,
		BackendSSL:        site.HostSSL,
		RequestTimeout:    siteRequestTimeout(site, resolveRequestTimeout(site.Path)),
		MaxUploadMB:       site.MaxUploadMB,
		StaticCacheDays:   site.StaticCacheDays,
		ResponseHeaders:   site.ResponseHeaders,
		CanonicalHost:     site.CanonicalHost,
		Staging:           site.Staging,
		RedirectTo:        site.RedirectTo,
		RedirectPermanent: site.RedirectPermanent,
		Redirects:         site.Redirects,
	}
	if ssl {
		data.CertDomain = site.PrimaryDomain()
	}

	rendered, err := renderVhost(tmpl, data)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	confPath := filepath.Join(config.NginxConfD(), confName)
	config.GuardRealWrite(confPath)
	return commitVhost(confPath, rendered)
}

// landingVhostConf renders a minimal vhost for site that serves htmlFile (read
// from pausedDir) for every path. Shared by the paused and the idle-waking
// landing pages. Secured sites get an 80->443 redirect plus a TLS server block;
// plain sites a single 80 server.
//
// Both carry the ACME challenge location, and the secured one carries it ahead
// of the redirect. A paused site still holds a certificate and that certificate
// still expires, so a site paused past its 30-day window has to be able to
// answer a renewal; without this the authority would follow the redirect to 443
// and be handed the paused page instead of the token.
func landingVhostConf(site config.Site, pausedDir, htmlFile string) string {
	serverNames := serverNames(site.Domains)
	if site.Secured {
		return fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %s;
%s
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name %s;
%s
    ssl_certificate /etc/nginx/certs/%s.crt;
    ssl_certificate_key /etc/nginx/certs/%s.key;
    root %s;
    location / {
        try_files /%s =503;
        default_type text/html;
    }
}
`, serverNames, acmeChallengeLocation, serverNames, tlsBlockFor(site.PrimaryDomain()), site.PrimaryDomain(), site.PrimaryDomain(), nginxQuote(pausedDir), htmlFile)
	}
	return fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %s;
%s
    root %s;
    location / {
        try_files /%s =503;
        default_type text/html;
    }
}
`, serverNames, acmeChallengeLocation, nginxQuote(pausedDir), htmlFile)
}

// writeLandingVhost writes site's static-page vhost (serving htmlFile) to
// conf.d/<domain>.conf and, for secured sites, removes the separate -ssl.conf so
// nginx stops routing HTTPS to the real backend while the page is up.
func writeLandingVhost(site config.Site, htmlFile string) error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	conf := landingVhostConf(site, config.PausedDir(), htmlFile)
	confPath := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	config.GuardRealWrite(confPath)
	if err := os.WriteFile(confPath, []byte(conf), 0644); err != nil {
		return err
	}
	if site.Secured {
		_ = os.Remove(filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf"))
	}
	return nil
}

// GeneratePausedVhost writes a minimal nginx vhost that serves the static paused
// landing page for the given site. For secured sites it also adds the HTTPS block
// so the redirect and TLS still work while the site is paused.
func GeneratePausedVhost(site config.Site) error {
	return writeLandingVhost(site, "paused.html")
}

// RemoveVhost deletes the vhost config files for the given domain.
func RemoveVhost(domain string) error {
	confD := config.NginxConfD()
	for _, suffix := range []string{".conf", "-ssl.conf"} {
		path := filepath.Join(confD, domain+suffix)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ErrNotRunning reports that servlo-nginx is down, so there is no process to
// signal. The on-disk config is still authoritative: whoever starts nginx next
// reads it. Callers that only need the config correct (the install reconcile,
// which starts nginx later in the same run) treat this as benign.
var ErrNotRunning = errors.New("servlo-nginx is not running")

var (
	containerRunningFn = podman.ContainerRunning
	reloadExecFn       = func() error {
		_, err := podman.Run("exec", "servlo-nginx", "nginx", "-s", "reload")
		return err
	}
)

// Reload signals nginx to reload its configuration. A failed signal is
// classified after the fact rather than pre-checked, so the common path costs
// no extra inspect and a genuine podman failure is never mistaken for a
// stopped container.
func Reload() error {
	err := reloadExecFn()
	if err == nil {
		return nil
	}
	if running, rerr := containerRunningFn("servlo-nginx"); rerr == nil && !running {
		return ErrNotRunning
	}
	return err
}

// ReloadWithRetry reloads nginx, retrying on failure for up to timeout. The cert
// file is now swapped in atomically, but the cert and key are still two separate
// renames, so a concurrent reissue (the watcher or UI reacting to the same site
// change) can momentarily pair a new cert with the old key. A reload that lands
// in that window crashes with "cannot load certificate"; retrying a beat later,
// once the swap has settled, succeeds. nginx keeps serving its previous config
// across a rejected reload, so retrying is safe.
func ReloadWithRetry(timeout time.Duration) error {
	return reloadWithRetry(Reload, timeout)
}

func reloadWithRetry(reload func() error, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := reload()
		// A stopped nginx will not come back within the window; retrying only
		// stalls the caller for the full timeout before failing anyway.
		if err == nil || errors.Is(err, ErrNotRunning) {
			return err
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Test runs `nginx -t` inside the servlo-nginx container and returns the
// combined stdout+stderr output along with the exit error. nginx writes its
// per-directive validation diagnostics to stderr, so the output is the
// useful payload regardless of success or failure, and callers should
// surface it as-is.
//
// The exec is bounded by a 10 second context so a paused/stuck container
// cannot wedge the calling HTTP handler indefinitely; nginx -t against a
// healthy container completes in well under a second. On timeout the
// returned error wraps context.DeadlineExceeded and the buffer carries
// whatever podman managed to emit before the deadline fired.
func Test() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := podman.CmdContext(ctx, "exec", "servlo-nginx", "nginx", "-t")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("nginx -t timed out after 10s: %w", ctx.Err())
	}
	return out, err
}

// VhostRepair describes a single vhost that was repaired during pre-flight.
type VhostRepair struct {
	Domain string
	Reason string // "missing-cert" or "orphan-ssl"
}

// RepairVhosts performs pre-flight validation of nginx vhost configs before start.
// It fixes SSL vhosts that reference cert files that don't exist on the host:
//
//   - If the domain belongs to a registered site, the vhost is regenerated as
//     plain HTTP and the site registry is updated (Secured = false).
//   - If no matching site exists (orphan SSL vhost), the config is removed.
//
// Plain HTTP vhosts are left untouched even if they don't match any site — they
// are harmless and may belong to parked or ignored sites.
func RepairVhosts() []VhostRepair {
	certsDir := filepath.Join(config.CertsDir(), "sites")
	confDir := config.NginxConfD()
	entries, err := os.ReadDir(confDir)
	if err != nil {
		return nil
	}

	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}

	var repairs []VhostRepair
	dirty := false

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		// Skip internal configs (default catch-all and servlo dashboard proxy).
		if entry.Name() == "_default.conf" || entry.Name() == "servlo.localhost.conf" {
			continue
		}

		confPath := filepath.Join(confDir, entry.Name())
		domain := strings.TrimSuffix(entry.Name(), ".conf")

		data, err := os.ReadFile(confPath)
		if err != nil {
			continue
		}

		// Only act on vhosts with missing TLS certificates — those crash nginx.
		if !hasMissingCert(string(data), certsDir) {
			continue
		}

		repaired := false
		for i, site := range reg.Sites {
			if site.PrimaryDomain() != domain || !site.Secured {
				continue
			}
			// Regenerate as plain HTTP vhost.
			var regenErr error
			switch {
			case site.IsHostProxy():
				regenErr = GenerateHostProxyVhost(site)
			case site.IsCustomContainer():
				regenErr = GenerateCustomVhost(site)
			case site.IsFrankenPHP():
				regenErr = GenerateFrankenPHPVhost(site)
			default:
				regenErr = GenerateVhost(site, site.PHPVersion)
			}
			if regenErr != nil {
				continue
			}
			reg.Sites[i].Secured = false
			dirty = true
			repaired = true
			repairs = append(repairs, VhostRepair{Domain: domain, Reason: "missing-cert"})
			os.Remove(filepath.Join(certsDir, domain+".crt")) //nolint:errcheck
			os.Remove(filepath.Join(certsDir, domain+".key")) //nolint:errcheck
			break
		}
		if !repaired {
			// No matching site — orphan SSL vhost with missing cert, remove it.
			os.Remove(confPath) //nolint:errcheck
			repairs = append(repairs, VhostRepair{Domain: domain, Reason: "orphan-ssl"})
		}
	}

	if dirty {
		config.SaveSites(reg) //nolint:errcheck
	}

	return repairs
}

// hasMissingCert returns true if the vhost content contains an ssl_certificate
// directive pointing to a cert file that doesn't exist on the host.
func hasMissingCert(content, certsDir string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ssl_certificate ") {
			continue
		}
		certPath := strings.TrimSuffix(strings.TrimPrefix(line, "ssl_certificate "), ";")
		certPath = strings.TrimSpace(certPath)
		hostPath := filepath.Join(certsDir, filepath.Base(certPath))
		if _, err := os.Stat(hostPath); os.IsNotExist(err) {
			return true
		}
	}
	return false
}

// defaultVhostManagedHashSuffix is appended to the conf filename to form
// a sentinel that records the sha256 of what servlo last wrote. nginx
// ignores files that don't match its `*.conf` include glob, so the
// sentinel stays out of the way.
const defaultVhostManagedHashSuffix = ".servlo-managed-hash"

// EnsureDefaultVhost writes a catch-all default server that shows a branded
// error page for any HTTP request that doesn't match a registered site. For
// HTTPS we cannot serve a real catch-all because browsers (Chrome especially)
// reject TLD-level wildcard certificates like `*.com` with
// ERR_CERT_COMMON_NAME_INVALID, and we can't issue per-domain certs ahead of
// time. ssl_reject_handshake produces a clean connection error
// (ERR_SSL_UNRECOGNIZED_NAME_ALERT) which is the best UX available.
//
// The file is left alone when the user has manually edited it: servlo
// stores a sentinel hash of what it last wrote, and skips rewriting when
// the on-disk content no longer matches that hash. Removing the file (or
// the sentinel) restores servlo's automatic management.
func EnsureDefaultVhost() error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	if err := writeErrorPages(); err != nil {
		return fmt.Errorf("writing error pages: %w", err)
	}

	canonical := renderDefaultVhost()
	path := filepath.Join(config.NginxConfD(), "_default.conf")
	sentinelPath := path + defaultVhostManagedHashSuffix

	canonicalHash := contentHashHex(canonical)

	onDisk, readErr := os.ReadFile(path)
	if errors.Is(readErr, os.ErrNotExist) {
		if err := WriteFileAtomic(path, canonical, 0644); err != nil {
			return err
		}
		return WriteFileAtomic(sentinelPath, []byte(canonicalHash), 0644)
	}
	if readErr != nil {
		return readErr
	}

	onDiskHash := contentHashHex(onDisk)
	lastWritten := strings.TrimSpace(readFileOrEmpty(sentinelPath))
	if lastWritten == "" {
		// Sentinel missing: sentinel-write crash (reclaim if hashes match),
		// pre-sentinel binary upgrade, or hand edit. The last two are
		// indistinguishable, so preserve and tell the user how to opt back in.
		if onDiskHash == canonicalHash {
			return WriteFileAtomic(sentinelPath, []byte(canonicalHash), 0644)
		}
		fmt.Printf("  [INFO] %s has no servlo sentinel; preserving on-disk content. If this is from a servlo upgrade and you haven't edited it, run: rm %s\n", path, path)
		return nil
	}
	if lastWritten != onDiskHash {
		fmt.Printf("  [INFO] %s differs from servlo's recorded last-write; preserving your edits. Remove the file to restore servlo's catch-all.\n", path)
		return nil
	}
	if onDiskHash == canonicalHash {
		return nil
	}
	// On-disk matches what servlo last wrote, but the template moved on.
	if err := WriteFileAtomic(path, canonical, 0644); err != nil {
		return err
	}
	return WriteFileAtomic(sentinelPath, []byte(canonicalHash), 0644)
}

// readFileOrEmpty returns the file's contents as a string, or "" on any
// error. Used for the sentinel read so a missing file and an unreadable
// file collapse to the same "no recorded last-write" state.
func readFileOrEmpty(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// WriteFileAtomic writes data to path via a sibling .tmp file followed by
// a rename, so a crash mid-write can never leave nginx pointing at a
// half-written conf. Preserves the destination file's mode if it already
// existed so an out-of-band chmod survives the rewrite; uses the caller's
// mode only when creating from scratch. Temp file is removed on any error.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	effective := mode
	if info, err := os.Stat(path); err == nil {
		effective = info.Mode().Perm()
	}
	tmp := path + ".tmp"
	config.GuardRealWrite(tmp)
	if err := os.WriteFile(tmp, data, effective); err != nil {
		return err
	}
	if err := os.Chmod(tmp, effective); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// CatchAllHeader is set by the catch-all default server on every response it
// serves, and by nothing else. A request that reaches this server matched no
// registered site: either the domain is not linked, or it is linked and nginx
// has not reloaded yet.
//
// The distinction matters to anything that waits for a site to come up. Servlo
// answers an unlinked domain with a branded page rather than refusing the
// connection, so "the domain answers over HTTP" is true well before the site
// behind it exists. Waiters that took an answer as readiness drove application
// installers against a site that was still the catch-all.
const (
	CatchAllHeader      = "X-Servlo-Site"
	CatchAllHeaderValue = "none"
)

// renderDefaultVhost returns the canonical _default.conf content.
// Separate from the writer so callers (and tests) can compute the same
// bytes servlo would write without touching disk.
func renderDefaultVhost() []byte {
	errorDir := config.ErrorPagesDir()
	return []byte(fmt.Sprintf(`server {
    listen 80 default_server;
    listen [::]:80 default_server;
    root %s;
    location / {
        add_header %s %s always;
        try_files /404.html =404;
        default_type text/html;
    }
}
server {
    listen 443 default_server ssl;
    listen [::]:443 default_server ssl;
    ssl_reject_handshake on;
}
`, nginxQuote(errorDir), CatchAllHeader, CatchAllHeaderValue))
}

// contentHashHex is sha256 → hex, used as the managed-file sentinel value.
func contentHashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

const errorPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Site Not Found — Servlo</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; }
    body {
      background: #0f1117;
      color: #e5e7eb;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      margin: 0;
    }
    .card {
      background: #1a1d27;
      border: 1px solid #2d3142;
      border-radius: 14px;
      padding: 2.5rem 3rem;
      max-width: 420px;
      width: calc(100% - 2rem);
      text-align: center;
    }
    .logo {
      width: 48px;
      height: 48px;
      margin: 0 auto 1.25rem;
      background: #FF2D20;
      border-radius: 12px;
      display: flex;
      align-items: center;
      justify-content: center;
      font-weight: 700;
      font-size: 1.2rem;
      color: #fff;
    }
    h1 { font-size: 1.2rem; font-weight: 600; margin: 0 0 0.5rem; }
    .host {
      font-size: 0.85rem;
      color: #FF2D20;
      font-family: ui-monospace, 'Cascadia Code', monospace;
      margin: 0 0 1rem;
      word-break: break-all;
    }
    p {
      font-size: 0.85rem;
      color: #9ca3af;
      margin: 0 0 1.5rem;
      line-height: 1.5;
    }
    code {
      background: #262a36;
      padding: 0.15rem 0.4rem;
      border-radius: 4px;
      font-size: 0.8rem;
      font-family: ui-monospace, 'Cascadia Code', monospace;
      color: #e5e7eb;
    }
    .actions { display: flex; gap: 0.5rem; }
    a, button {
      flex: 1;
      display: inline-block;
      text-decoration: none;
      text-align: center;
      border-radius: 8px;
      padding: 0.6rem 0;
      font-size: 0.85rem;
      font-weight: 500;
      cursor: pointer;
      transition: background 0.15s;
      border: none;
    }
    .btn-primary { background: #FF2D20; color: #fff; }
    .btn-primary:hover { background: #e02419; }
    .btn-secondary { background: #262a36; color: #e5e7eb; border: 1px solid #2d3142; }
    .btn-secondary:hover { background: #2d3142; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">L</div>
    <h1>Site Not Found</h1>
    <p class="host" id="host"></p>
    <p>This domain is not linked to any site. Run <code>servlo link</code> in your project directory to register it.</p>
    <div class="actions">
      <a id="dashboard-link" href="#" class="btn-primary">Open Dashboard</a>
      <button class="btn-secondary" onclick="location.reload()">Retry</button>
    </div>
  </div>
  <script>
    document.getElementById('host').textContent = location.hostname;
    // The dashboard runs on servlo-panel at port 7073 on the same host the visitor
    // already reached. Using location.hostname (rather than a hardcoded
    // servlo.localhost) means LAN clients get a working link to the server's
    // address, not their own loopback.
    document.getElementById('dashboard-link').href = location.protocol + '//' + location.hostname + ':7073/';
  </script>
</body>
</html>
`

// writeErrorPages ensures the error page HTML files exist in the error pages directory.
func writeErrorPages() error {
	dir := config.ErrorPagesDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	config.GuardRealWrite(filepath.Join(dir, "404.html"))
	return os.WriteFile(filepath.Join(dir, "404.html"), []byte(errorPageHTML), 0644)
}

// EnsureServloVhost generates the nginx vhost for http://servlo.localhost,
// which reverse-proxies to the servlo-panel process running on the host so the
// browser's URL bar stays on servlo.localhost (no redirect to localhost:7073).
//
// The upstream differs by platform because container → host connectivity
// works differently on each:
//
//   - Linux: servlo-nginx runs in a rootless podman bridge. Reaching the
//     host over TCP via host.containers.internal depends on netavark /
//     pasta wiring up the 169.254.1.2 alias, which silently breaks
//     across podman versions and host network changes. We bind-mount
//     servlo-panel's unix socket into the container instead — filesystem
//     access only, no networking, no detection. servlo-panel marks
//     socket-arriving requests as loopback in isLoopbackRequest.
//
//   - macOS: servlo-panel runs as a native macOS process and servlo-nginx runs
//     inside the podman-machine VM. Unix sockets don't traverse the
//     virtio-fs / 9p hypervisor boundary as functional sockets, so
//     binding one on the macOS host doesn't help the VM. We fall back
//     to TCP via host.containers.internal:7073 — gvproxy reliably
//     forwards this on podman-machine, and the request carries an
//     X-Servlo-Trust header that the gate matches against the per-install
//     token (proxy_set_header overwrites any client-supplied value, so
//     a LAN attacker can't inject it).
//
// .localhost is RFC 6761 reserved and always resolves to the visiting
// device's loopback, so this vhost is unreachable from a LAN browser doing
// the obvious thing (http://servlo.localhost from a remote machine hits the
// remote machine's own 127.0.0.1, not the servlo server).
func EnsureServloVhost() error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}

	content := fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name servlo.localhost;

    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    location = / {
        proxy_pass http://unix:%[1]s:;
    }

    # The API is proxied through this vhost rather than reached directly on
    # port 7073, so the dashboard is one origin. A session cookie is
    # SameSite=Strict and would never be attached to a cross-origin request,
    # which is what the split-origin arrangement used to require.
    location ^~ /api/ {
        proxy_pass http://unix:%[1]s:$request_uri;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_read_timeout 3600s;
    }

    location ^~ /icons/ {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location ^~ /assets/ {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location ^~ /_svc/ {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location = /manifest.webmanifest {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location = /sw.js {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location = /offline.html {
        proxy_pass http://unix:%[1]s:$request_uri;
    }

    location / {
        return 444;
    }
}
`, config.UISocketPath())
	config.GuardRealWrite(filepath.Join(config.NginxConfD(), "servlo.localhost.conf"))
	return os.WriteFile(filepath.Join(config.NginxConfD(), "servlo.localhost.conf"), []byte(content), 0644)
}

// EnsureNginxConfig copies the base nginx.conf to the data dir if it is missing.
func EnsureNginxConfig() error {
	nginxDir := config.NginxDir()
	if err := os.MkdirAll(nginxDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	if err := EnsureCustomD(); err != nil {
		return err
	}
	if err := EnsureHttpD(); err != nil {
		return err
	}
	if err := EnsureForwardedConf(); err != nil {
		return err
	}
	if err := EnsureChallengeDir(); err != nil {
		return err
	}
	if err := EnsureContainerMounts(); err != nil {
		return err
	}

	destPath := filepath.Join(nginxDir, "nginx.conf")
	tmplData, err := GetTemplate("nginx.conf")
	if err != nil {
		return fmt.Errorf("failed to read embedded nginx.conf: %w", err)
	}
	tmpl, err := template.New("nginx.conf").Parse(string(tmplData))
	if err != nil {
		return fmt.Errorf("parsing nginx.conf template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nginxConfData{
		Resolver:        podman.NetworkGateway("servlo"),
		AccessLogTarget: config.AccessLogTarget(),
	}); err != nil {
		return fmt.Errorf("rendering nginx.conf: %w", err)
	}
	rendered := dropOverriddenDefaults(buf.String(), httpOverrideNames())
	config.GuardRealWrite(destPath)
	return os.WriteFile(destPath, []byte(rendered), 0644)
}

// forwardedConf declares $real_forwarded_host / $real_forwarded_proto /
// $real_forwarded_port at http{} level. Each falls back to the local
// connection value when no X-Forwarded-* header is present, so direct
// browser access still works. The port map fixes URLs that frameworks
// (Ziggy, Symfony Request::getSchemeAndHttpHost) compute from
// SERVER_PORT — without it a tunneled/LAN-shared site emits absolute
// URLs with the nginx listen port (e.g. http://<ip>:443/foo).
const forwardedConf = `# Generated by servlo. Declares variables used by per-site vhosts.
# Edit user overrides in ~/.local/share/servlo/nginx/custom.d/ instead.
map $http_x_forwarded_host $real_forwarded_host {
    default $http_x_forwarded_host;
    ""      $host;
}

map $http_x_forwarded_proto $real_forwarded_proto {
    default $http_x_forwarded_proto;
    ""      $scheme;
}

map $http_x_forwarded_port $real_forwarded_port {
    default $http_x_forwarded_port;
    ""      $server_port;
}
`

// EnsureForwardedConf writes the shared _forwarded.conf snippet into
// conf.d. The "_" prefix makes it load before site vhosts.
func EnsureForwardedConf() error {
	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		return err
	}
	content := forwardedConf
	config.GuardRealWrite(filepath.Join(config.NginxConfD(), "_forwarded.conf"))
	return os.WriteFile(
		filepath.Join(config.NginxConfD(), "_forwarded.conf"),
		[]byte(content),
		0644,
	)
}

// EnsureCustomD creates the user-override directory. Servlo never writes here
// after creation, so user snippets survive `servlo update`.
func EnsureCustomD() error {
	return os.MkdirAll(config.NginxCustomD(), 0755)
}

// EnsureHttpD creates the http.d directory for user http-level overrides so the
// nginx.conf `include /etc/nginx/http.d/*.conf;` always resolves, even before
// the user has added any override.
func EnsureHttpD() error {
	return os.MkdirAll(config.NginxHttpD(), 0755)
}

// RewriteNginxQuadlet rewrites servlo-nginx.container from the bundled template
// and reports whether the on-disk quadlet actually changed. Callers use this
// to bring installs that pre-date a template change (e.g. the new http.d
// mount) up to current shape on demand, instead of waiting for the next
// `servlo start` / `servlo update`. The http config editor calls it before
// writing the user override so the freshly written file is actually mounted
// into the running nginx container — without this heal, the file would be
// orphaned on disk and silently ignored.
func RewriteNginxQuadlet() (changed bool, err error) {
	content, err := podman.GetQuadletTemplate("servlo-nginx.container")
	if err != nil {
		return false, fmt.Errorf("reading bundled nginx quadlet template: %w", err)
	}
	// The template carries only the servlo-owned mounts, so a site or parked
	// directory outside $HOME must have its Volume= line re-injected here, the
	// same way RewriteFPMQuadlets does, or nginx restarts without the docroot.
	content = podman.InjectExtraVolumes(content, podman.ExtraVolumePaths())
	httpPort, httpsPort := podman.ConfiguredHostPorts()
	content = podman.ApplyHostPorts(content, httpPort, httpsPort)
	return podman.WriteQuadletDiff("servlo-nginx", content)
}
