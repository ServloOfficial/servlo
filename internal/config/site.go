package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Site represents a single registered Servlo site.
type Site struct {
	Name        string   `yaml:"-"`
	Domains     []string `yaml:"-"`
	Path        string   `yaml:"path"`
	PHPVersion  string   `yaml:"php_version"`
	NodeVersion string   `yaml:"node_version"`
	Secured     bool     `yaml:"secured"`
	// SecuredBeforeDNSOff records that the site was on HTTPS when servlo DNS was
	// last disabled, so re-enabling can restore it even for a site with no
	// .servlo.yaml to carry the intent. Cleared once HTTPS is restored.
	SecuredBeforeDNSOff bool     `yaml:"secured_before_dns_off,omitempty"`
	Ignored             bool     `yaml:"ignored,omitempty"`
	Paused              bool     `yaml:"paused,omitempty"`
	PausedWorkers       []string `yaml:"paused_workers,omitempty"`
	// Pinned keeps the site at the top of the list even
	// when the global idle policy is on, so a site you want always-warm never
	// sleeps.
	Pinned    bool   `yaml:"pinned,omitempty"`
	Framework string `yaml:"framework,omitempty"`
	PublicDir string `yaml:"public_dir,omitempty"`
	// AppURL, when set, is the per-machine override for APP_URL in the
	// project's env file. Lower priority than ProjectConfig.AppURL (which is
	// committed to the repo) and higher priority than the default generator
	// (`<scheme>://<primary-domain>`). Use this for personal customizations
	// you don't want to share via .servlo.yaml.
	AppURL string `yaml:"app_url,omitempty"`
	// LANPort, when non-zero, means a host-level reverse proxy is (or should
	// be) listening on 0.0.0.0:LANPort, forwarding to the site with the Host
	// header rewritten. LAN devices can reach the site at <lanIP>:LANPort
	// without any DNS configuration.
	LANPort int `yaml:"lan_port,omitempty"`
	// DevServerPort, when non-zero, is the host port the site's dev server is
	// pinned to. It has to be stable and known ahead of time, because the
	// site's vhost proxies to it and the tool would otherwise drift to the
	// next free port whenever several sites run at once.
	DevServerPort int `yaml:"dev_server_port,omitempty"`
	// ContainerPort, when non-zero, means this site uses a per-project custom
	// container instead of the shared PHP-FPM image. The value is the port the
	// app listens on inside the container; nginx reverse-proxies to it.
	ContainerPort int `yaml:"container_port,omitempty"`
	// ContainerSSL, when true, means the app inside the custom container serves
	// TLS on its port; nginx will proxy_pass via HTTPS with ssl_verify off.
	ContainerSSL bool `yaml:"container_ssl,omitempty"`
	// Runtime is "fpm" (default) or "frankenphp". When "frankenphp" the site
	// runs a per-site dunglas/frankenphp:php<version> container and nginx
	// reverse-proxies to it on port 8000.
	Runtime string `yaml:"runtime,omitempty"`
	// RuntimeWorker toggles FrankenPHP worker mode when Runtime=="frankenphp".
	RuntimeWorker bool `yaml:"runtime_worker,omitempty"`
	// HostPort, when non-zero, means this site is a host-proxy site: it has no
	// container, and nginx reverse-proxies the domain to a process running on
	// the host (the dev server) listening on this port.
	HostPort int `yaml:"host_port,omitempty"`
	// HostSSL, when true, means the host process serves TLS on its port; nginx
	// proxies via HTTPS with ssl_verify off.
	HostSSL bool `yaml:"host_ssl,omitempty"`
	// HostCommand is the dev command servlo supervises for a host-proxy site
	// (e.g. "npm run start:dev"). Empty means proxy-only: the user runs the
	// server themselves and servlo only wires the proxy.
	HostCommand string `yaml:"host_command,omitempty"`
	// ApprovedCommands holds exact command strings the user has consented to run
	// on the host for this site (project-origin custom host workers and commands).
	// Keyed by the exact string so a changed command re-prompts.
	ApprovedCommands []string `yaml:"approved_commands,omitempty"`
	// Group is the group key shared by a main site and its secondaries. It is
	// set to the main site's name. Empty when the site is not grouped.
	Group string `yaml:"group,omitempty"`
	// GroupSubdomain is the subdomain label a secondary occupies on the group
	// main's base domain (e.g. "admin" -> admin.<main-domain>). Empty on the
	// main site; non-empty identifies a secondary.
	GroupSubdomain string `yaml:"group_subdomain,omitempty"`
	// GroupSharedDB, when true on a secondary, means the site shares the group
	// main's database instead of its own: DB_DATABASE in its .env is kept in
	// sync with the main's database name.
	GroupSharedDB bool `yaml:"group_shared_db,omitempty"`
	// StandaloneDomain is the domain a secondary served before it joined a
	// group, kept so leaving one restores exactly that. It used to be
	// re-derived from the directory name, which only worked while servlo had a
	// TLD to append; a real domain cannot be guessed back.
	StandaloneDomain string `yaml:"standalone_domain,omitempty"`

	// MaxUploadMB is the site's upload ceiling. One field because it is one
	// decision that has to reach three directives: upload_max_filesize and
	// post_max_size in the site's PHP-FPM pool, client_max_body_size in its
	// vhost. Raise only some and whichever stayed low refuses the upload, which
	// reads to an operator as the setting not working. Zero leaves every one of
	// them to its default.
	MaxUploadMB int `yaml:"max_upload_mb,omitempty"`
	// MaxExecutionSeconds is how long a request may run. Also one decision in
	// two places: PHP's max_execution_time and nginx's fastcgi read and send
	// timeouts. Whichever is lower is the one the visitor experiences, so an
	// import set to 600 in PHP alone still dies at nginx's 60.
	MaxExecutionSeconds int `yaml:"max_execution_seconds,omitempty"`
	// MemoryLimitMB is the site's PHP memory_limit. Zero leaves the default.
	MemoryLimitMB int `yaml:"memory_limit_mb,omitempty"`

	// StaticCacheDays is how long a browser may keep this site's static
	// assets. Zero leaves nginx's default, which is to say nothing and let the
	// browser revalidate.
	StaticCacheDays int `yaml:"static_cache_days,omitempty"`
	// ResponseHeaders are headers added to every response from this site.
	ResponseHeaders []ResponseHeader `yaml:"response_headers,omitempty"`
	// CanonicalHost names which of a site's two www forms is the real one:
	// "www" or "apex". The other is permanently redirected to it. Empty leaves
	// both serving, which is the default and what a site that never asked for
	// one keeps.
	CanonicalHost string `yaml:"canonical_host,omitempty"`

	// RedirectTo is where this whole domain has moved. Everything on the site
	// goes there, path and query kept. Empty means the site serves itself.
	RedirectTo string `yaml:"redirect_to,omitempty"`
	// RedirectPermanent makes the whole-domain redirect a 301 rather than a
	// 302. Off by default, because a 301 is cached by browsers and is not
	// something an operator can take back.
	RedirectPermanent bool `yaml:"redirect_permanent,omitempty"`
	// Redirects are single addresses that have moved, matched exactly.
	Redirects []Redirect `yaml:"redirects,omitempty"`

	// DeployExclude names paths a deploy of this site must not remove,
	// relative to the site root. A pointer because unset and empty are
	// different answers: unset follows the framework's list, and an empty list
	// is an operator saying this site protects nothing.
	DeployExclude *[]string `yaml:"deploy_exclude,omitempty"`

	// Database names the connection this site's data lives on. Empty means the
	// install's default, which is what every site had before a database could
	// be anywhere but the container next door.
	Database string `yaml:"database,omitempty"`
}

// Redirect is one address that has moved.
type Redirect struct {
	// From is a path on this site, matched exactly.
	From string `yaml:"from"`
	// To is a path on this site or an absolute http(s) URL.
	To string `yaml:"to"`
	// Permanent makes it a 301 rather than a 302.
	Permanent bool `yaml:"permanent,omitempty"`
}

// ResponseHeader is one header a site adds to its responses.
type ResponseHeader struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

// Ceilings for the per-site PHP settings. A value past one of these is a
// mistake, and writing it produces a pool FPM refuses to start with, which
// takes down every site sharing the container.
const (
	MaxUploadCeilingMB   = 16384
	MaxExecutionCeilingS = 86400
	MemoryLimitCeilingMB = 65536
	// StaticCacheCeilingDays is a year, which is already the longest anyone
	// sensibly caches an asset. Past it the number is a typo.
	StaticCacheCeilingDays = 365
)

// headerName is a field name as HTTP defines one: a token, no separators. It
// lands unquoted in an add_header directive, so anything else is refused.
var headerName = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_` + "`" + `|~-]{1,64}$`)

// servloOwnedHeaders are headers servlo writes itself. add_header appends
// rather than replaces, so a second one is emitted alongside, and for HSTS a
// browser is entitled to honour the shorter max-age: the form could quietly
// weaken what the TLS block set.
var servloOwnedHeaders = map[string]string{
	"strict-transport-security": "Strict-Transport-Security",
}

// ValidateNginxSettings refuses a per-site nginx setting that would not survive
// contact with the directive it lands in. nginx loads its whole configuration
// or none of it, so a value that ends a directive early takes every site on the
// machine down, not only this one.
func (s *Site) ValidateNginxSettings() error {
	if s.StaticCacheDays < 0 || s.StaticCacheDays > StaticCacheCeilingDays {
		return fmt.Errorf("the static cache window %d days is outside 0 to %d", s.StaticCacheDays, StaticCacheCeilingDays)
	}
	seen := map[string]bool{}
	for _, h := range s.ResponseHeaders {
		if !headerName.MatchString(h.Name) {
			return fmt.Errorf("%q is not a usable header name", h.Name)
		}
		key := strings.ToLower(h.Name)
		if canonical, owned := servloOwnedHeaders[key]; owned {
			return fmt.Errorf("servlo writes %s itself from this site's TLS state, and a second one would be sent alongside it", canonical)
		}
		if seen[key] {
			return fmt.Errorf("%s is set twice, which would send it twice", h.Name)
		}
		seen[key] = true
		// The value goes inside quotes in an add_header directive. A quote ends
		// the string it sits in; a brace or a semicolon ends the directive and
		// starts whatever the value says next.
		if i := strings.IndexAny(h.Value, "\"\\{};#\n\r\x00"); i >= 0 {
			return fmt.Errorf("the value for %s contains %q, which would end the directive it is written into", h.Name, string(h.Value[i]))
		}
	}
	return nil
}

// ValidatePHPSettings refuses a per-site setting that is out of range. These
// reach the site from the panel and land in a pool and an nginx directive, and
// either file failing to parse takes more than this site down with it.
func (s *Site) ValidatePHPSettings() error {
	switch {
	case s.MaxUploadMB < 0 || s.MaxUploadMB > MaxUploadCeilingMB:
		return fmt.Errorf("the max upload size %dM is outside 0 to %dM", s.MaxUploadMB, MaxUploadCeilingMB)
	case s.MaxExecutionSeconds < 0 || s.MaxExecutionSeconds > MaxExecutionCeilingS:
		return fmt.Errorf("the max execution time %ds is outside 0 to %ds", s.MaxExecutionSeconds, MaxExecutionCeilingS)
	case s.MemoryLimitMB < 0 || s.MemoryLimitMB > MemoryLimitCeilingMB:
		return fmt.Errorf("the memory limit %dM is outside 0 to %dM", s.MemoryLimitMB, MemoryLimitCeilingMB)
	}
	return nil
}

// IsGroupMain returns true when the site owns a group's base domain: it has a
// group key but no subdomain of its own.
func (s *Site) IsGroupMain() bool {
	return s.Group != "" && s.GroupSubdomain == ""
}

// IsGroupSecondary returns true when the site occupies a subdomain of its
// group main's base domain.
func (s *Site) IsGroupSecondary() bool {
	return s.Group != "" && s.GroupSubdomain != ""
}

// IsCustomContainer returns true when the site uses a per-project custom
// container instead of the shared PHP-FPM image.
func (s *Site) IsCustomContainer() bool {
	return s.ContainerPort > 0
}

// IsFrankenPHP returns true when the site is served by a per-site
// dunglas/frankenphp container instead of the shared PHP-FPM image.
func (s *Site) IsFrankenPHP() bool {
	return s.Runtime == "frankenphp"
}

// IsCustomFPM returns true when the site is a PHP project served by fastcgi
// from its own per-site image, built from a Containerfile (a container: config
// with no port). It is a normal PHP-FPM site whose container is per-site.
func (s *Site) IsCustomFPM() bool {
	return s.Runtime == "fpm-custom"
}

// IsHostProxy returns true when the site is a host-proxy site: nginx
// reverse-proxies the domain to a host process instead of a container.
func (s *Site) IsHostProxy() bool {
	return s.HostPort > 0
}

// IsProxyOnly returns true when the site is a host-proxy site servlo runs nothing
// for: nginx forwards to a dev server the user starts themselves, so there is no
// supervised process to stop.
func (s *Site) IsProxyOnly() bool {
	return s.IsHostProxy() && s.HostCommand == ""
}

// HostProxyWorkerName is the worker name of a host-proxy site's supervised
// dev server. There is exactly one per site.
const HostProxyWorkerName = "app"

// StripeWorkerName is the Stripe webhook listener, run through its own unit
// (servlo-stripe-<site>) rather than declared by any framework.
const StripeWorkerName = "stripe"

// IsBuiltinWorker reports whether name is a servlo-managed worker that lives
// outside a framework's worker definitions: the Stripe listener and the
// host-proxy dev server. A validator checking a site's workers against its
// framework must treat these as valid rather than undefined, the same way the
// running-worker collector and the orphan scan already special-case them.
func IsBuiltinWorker(name string) bool {
	return name == StripeWorkerName || name == HostProxyWorkerName
}

// HostProxyWorkerUnit returns the worker unit name for a host-proxy site's dev
// server (servlo-app-<site>). Single source of truth for the cli (which starts
// and stops it) and siteinfo (which reports its health).
func HostProxyWorkerUnit(siteName string) string {
	return "servlo-" + HostProxyWorkerName + "-" + siteName
}

// PrimaryDomain returns the first (primary) domain for the site.
func (s *Site) PrimaryDomain() string {
	if len(s.Domains) > 0 {
		return s.Domains[0]
	}
	return ""
}

// HasDomain returns true if the site has the given domain.
func (s *Site) HasDomain(domain string) bool {
	for _, d := range s.Domains {
		if d == domain {
			return true
		}
	}
	return false
}

// siteYAML is the on-disk YAML representation of a Site, supporting both the
// legacy single "domain" field and the new "domains" array.
type siteYAML struct {
	Name                string           `yaml:"name"`
	Domain              string           `yaml:"domain,omitempty"`  // legacy single domain
	Domains             []string         `yaml:"domains,omitempty"` // new multi-domain
	Path                string           `yaml:"path"`
	PHPVersion          string           `yaml:"php_version"`
	NodeVersion         string           `yaml:"node_version"`
	Secured             bool             `yaml:"secured"`
	SecuredBeforeDNSOff bool             `yaml:"secured_before_dns_off,omitempty"`
	Ignored             bool             `yaml:"ignored,omitempty"`
	Paused              bool             `yaml:"paused,omitempty"`
	PausedWorkers       []string         `yaml:"paused_workers,omitempty"`
	Pinned              bool             `yaml:"pinned,omitempty"`
	Framework           string           `yaml:"framework,omitempty"`
	PublicDir           string           `yaml:"public_dir,omitempty"`
	AppURL              string           `yaml:"app_url,omitempty"`
	LANPort             int              `yaml:"lan_port,omitempty"`
	DevServerPort       int              `yaml:"dev_server_port,omitempty"`
	ContainerPort       int              `yaml:"container_port,omitempty"`
	ContainerSSL        bool             `yaml:"container_ssl,omitempty"`
	Runtime             string           `yaml:"runtime,omitempty"`
	RuntimeWorker       bool             `yaml:"runtime_worker,omitempty"`
	HostPort            int              `yaml:"host_port,omitempty"`
	HostSSL             bool             `yaml:"host_ssl,omitempty"`
	HostCommand         string           `yaml:"host_command,omitempty"`
	ApprovedCommands    []string         `yaml:"approved_commands,omitempty"`
	Group               string           `yaml:"group,omitempty"`
	GroupSubdomain      string           `yaml:"group_subdomain,omitempty"`
	GroupSharedDB       bool             `yaml:"group_shared_db,omitempty"`
	StandaloneDomain    string           `yaml:"standalone_domain,omitempty"`
	MaxUploadMB         int              `yaml:"max_upload_mb,omitempty"`
	MaxExecutionSeconds int              `yaml:"max_execution_seconds,omitempty"`
	MemoryLimitMB       int              `yaml:"memory_limit_mb,omitempty"`
	StaticCacheDays     int              `yaml:"static_cache_days,omitempty"`
	ResponseHeaders     []ResponseHeader `yaml:"response_headers,omitempty"`
	CanonicalHost       string           `yaml:"canonical_host,omitempty"`
	RedirectTo          string           `yaml:"redirect_to,omitempty"`
	RedirectPermanent   bool             `yaml:"redirect_permanent,omitempty"`
	Redirects           []Redirect       `yaml:"redirects,omitempty"`
	DeployExclude       *[]string        `yaml:"deploy_exclude,omitempty"`
	Database            string           `yaml:"database,omitempty"`
}

func (s Site) toYAML() siteYAML {
	return siteYAML{
		Name:                s.Name,
		Domains:             s.Domains,
		Path:                s.Path,
		PHPVersion:          s.PHPVersion,
		NodeVersion:         s.NodeVersion,
		Secured:             s.Secured,
		SecuredBeforeDNSOff: s.SecuredBeforeDNSOff,
		Ignored:             s.Ignored,
		Paused:              s.Paused,
		PausedWorkers:       s.PausedWorkers,
		Pinned:              s.Pinned,
		Framework:           s.Framework,
		PublicDir:           s.PublicDir,
		AppURL:              s.AppURL,
		LANPort:             s.LANPort,
		DevServerPort:       s.DevServerPort,
		ContainerPort:       s.ContainerPort,
		ContainerSSL:        s.ContainerSSL,
		Runtime:             s.Runtime,
		RuntimeWorker:       s.RuntimeWorker,
		HostPort:            s.HostPort,
		HostSSL:             s.HostSSL,
		HostCommand:         s.HostCommand,
		ApprovedCommands:    s.ApprovedCommands,
		Group:               s.Group,
		GroupSubdomain:      s.GroupSubdomain,
		GroupSharedDB:       s.GroupSharedDB,
		StandaloneDomain:    s.StandaloneDomain,
		MaxUploadMB:         s.MaxUploadMB,
		MaxExecutionSeconds: s.MaxExecutionSeconds,
		MemoryLimitMB:       s.MemoryLimitMB,
		StaticCacheDays:     s.StaticCacheDays,
		ResponseHeaders:     s.ResponseHeaders,
		CanonicalHost:       s.CanonicalHost,
		RedirectTo:          s.RedirectTo,
		RedirectPermanent:   s.RedirectPermanent,
		Redirects:           s.Redirects,
		DeployExclude:       s.DeployExclude,
		Database:            s.Database,
	}
}

func (sy siteYAML) toSite() Site {
	domains := sy.Domains
	if len(domains) == 0 && sy.Domain != "" {
		domains = []string{sy.Domain}
	}
	return Site{
		Name:                sy.Name,
		Domains:             domains,
		Path:                sy.Path,
		PHPVersion:          sy.PHPVersion,
		NodeVersion:         sy.NodeVersion,
		Secured:             sy.Secured,
		SecuredBeforeDNSOff: sy.SecuredBeforeDNSOff,
		Ignored:             sy.Ignored,
		Paused:              sy.Paused,
		PausedWorkers:       sy.PausedWorkers,
		Pinned:              sy.Pinned,
		Framework:           sy.Framework,
		PublicDir:           sy.PublicDir,
		AppURL:              sy.AppURL,
		LANPort:             sy.LANPort,
		DevServerPort:       sy.DevServerPort,
		ContainerPort:       sy.ContainerPort,
		ContainerSSL:        sy.ContainerSSL,
		Runtime:             sy.Runtime,
		RuntimeWorker:       sy.RuntimeWorker,
		HostPort:            sy.HostPort,
		HostSSL:             sy.HostSSL,
		HostCommand:         sy.HostCommand,
		ApprovedCommands:    sy.ApprovedCommands,
		Group:               sy.Group,
		GroupSubdomain:      sy.GroupSubdomain,
		GroupSharedDB:       sy.GroupSharedDB,
		StandaloneDomain:    sy.StandaloneDomain,
		MaxUploadMB:         sy.MaxUploadMB,
		MaxExecutionSeconds: sy.MaxExecutionSeconds,
		MemoryLimitMB:       sy.MemoryLimitMB,
		StaticCacheDays:     sy.StaticCacheDays,
		ResponseHeaders:     sy.ResponseHeaders,
		CanonicalHost:       sy.CanonicalHost,
		RedirectTo:          sy.RedirectTo,
		RedirectPermanent:   sy.RedirectPermanent,
		Redirects:           sy.Redirects,
		DeployExclude:       sy.DeployExclude,
		Database:            sy.Database,
	}
}

// SiteRegistry holds all registered sites.
type SiteRegistry struct {
	Sites []Site
}

type siteRegistryYAML struct {
	Sites []siteYAML `yaml:"sites"`
}

// sitesCache memoises the parsed registry keyed on sites.yaml's mtime+size.
// The daemon's snapshot path used to re-read and re-parse sites.yaml once per
// snapshot rebuild via LoadAll; with many sites this dominated the YAML parse
// cost. The cache returns a freshly-allocated registry so callers can mutate
// the slice without poisoning the cached value.
var (
	sitesCacheMu sync.Mutex
	sitesCache   *SiteRegistry
	sitesCacheAt time.Time
	sitesCacheSz int64
)

// siteWriteMu serializes every read-modify-write of the registry (AddSite,
// RemoveSite, ReorderSites, IgnoreSite). Each does LoadSites -> mutate ->
// SaveSites; without one lock spanning the whole sequence, concurrent writers
// (the idle engine's goroutines, a CLI pin/pause, a worker toggle) interleave
// and clobber each other, which let one site's worker list bleed onto another.
var siteWriteMu sync.Mutex

func invalidateSitesCache() {
	sitesCacheMu.Lock()
	sitesCache = nil
	sitesCacheAt = time.Time{}
	sitesCacheSz = 0
	sitesCacheMu.Unlock()
}

// LoadSites reads sites.yaml, returning an empty registry if the file does not exist.
func LoadSites() (*SiteRegistry, error) {
	path := SitesFile()
	info, statErr := os.Stat(path)

	sitesCacheMu.Lock()
	if sitesCache != nil && statErr == nil &&
		sitesCacheAt.Equal(info.ModTime()) && sitesCacheSz == info.Size() {
		out := cloneSiteRegistry(sitesCache)
		sitesCacheMu.Unlock()
		return out, nil
	}
	sitesCacheMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SiteRegistry{}, nil
		}
		return nil, err
	}

	var raw siteRegistryYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	reg := &SiteRegistry{Sites: make([]Site, len(raw.Sites))}
	for i, sy := range raw.Sites {
		reg.Sites[i] = sy.toSite()
	}

	if statErr == nil {
		sitesCacheMu.Lock()
		sitesCache = cloneSiteRegistry(reg)
		sitesCacheAt = info.ModTime()
		sitesCacheSz = info.Size()
		sitesCacheMu.Unlock()
	}
	return reg, nil
}

func cloneSiteRegistry(in *SiteRegistry) *SiteRegistry {
	if in == nil {
		return &SiteRegistry{}
	}
	out := &SiteRegistry{Sites: make([]Site, len(in.Sites))}
	for i, s := range in.Sites {
		cp := s
		if s.Domains != nil {
			cp.Domains = append([]string(nil), s.Domains...)
		}
		if s.PausedWorkers != nil {
			cp.PausedWorkers = append([]string(nil), s.PausedWorkers...)
		}
		out.Sites[i] = cp
	}
	return out
}

// SaveSites writes the registry to sites.yaml.
func SaveSites(reg *SiteRegistry) error {
	if err := os.MkdirAll(DataDir(), 0755); err != nil {
		return err
	}

	raw := siteRegistryYAML{Sites: make([]siteYAML, len(reg.Sites))}
	for i, s := range reg.Sites {
		raw.Sites[i] = s.toYAML()
	}

	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(SitesFile(), data, 0644); err != nil {
		return err
	}
	invalidateSitesCache()
	return nil
}

// writeFileAtomic writes data to path through a uniquely named temp file in the
// same directory followed by a rename. The rename is atomic on the same
// filesystem, so a crash, a restart mid-write, or a second concurrent writer can
// never leave sites.yaml half-written or interleaved; a reader always sees a
// complete file and the last full write wins. A unique temp name (rather than a
// fixed path.tmp) keeps two concurrent writers from clobbering each other's temp.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	guardRealWrite(path)
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// domainForbidden are the characters a domain may never carry. The nginx set
// (a directive ends at `;`, blocks open and close on braces, `#` comments the
// rest of the line) plus whitespace and the separators that would make one
// domain read as several, or as a path when it names a vhost file, plus the
// quotes that would close a string literal in any file a domain is written into.
const domainForbidden = "{};#\n\r\x00 \t/\\'\""

// AddSite appends or updates a site in the registry.
func AddSite(site Site) error {
	// A site name flows into systemd unit file names and bodies (Description=,
	// --env=SERVLO_SITE=, servlo-stripe-<name>, ...). Refuse newline/NUL (which
	// would inject a unit directive) and slash (which would escape the unit
	// path), closing the injection even for callers that bypass SiteNameAndDomain.
	if ContainsUnitInjectionChars(site.Name) || strings.ContainsRune(site.Name, '/') {
		return fmt.Errorf("invalid site name %q: must not contain newline, NUL, or slash", site.Name)
	}
	// A domain is written into the vhost's server_name, and a project's
	// .servlo.yaml supplies the list. The vhost generator refuses these too; this
	// is the registry side, so a bad domain is rejected when the site is linked
	// rather than when nginx is next rendered.
	for _, d := range site.Domains {
		if i := strings.IndexAny(d, domainForbidden); i >= 0 {
			return fmt.Errorf("invalid domain %q: must not contain %q", d, string(d[i]))
		}
	}
	// Store the resolved path so two spellings of one directory (on ostree hosts
	// /home is a symlink to /var/home, and os.Getwd can return either) register
	// and de-duplicate as a single site rather than two (#930).
	site.Path = CanonicalPath(site.Path)
	// The filesystem root can never be a site: servlo bind-mounts a site's path into
	// its containers, and mounting / over a container's own rootfs shadows its
	// entrypoint so it cannot start (issue #884).
	if filepath.Clean(site.Path) == "/" {
		return fmt.Errorf("invalid site path %q: servlo would bind-mount / into every container and shadow its rootfs", site.Path)
	}
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}

	for i, s := range reg.Sites {
		if s.Name == site.Name {
			reg.Sites[i] = site
			return SaveSites(reg)
		}
	}

	reg.Sites = append(reg.Sites, site)
	return SaveSites(reg)
}

// CommandApproved reports whether the user has consented to run command on the
// host for this site (see ApprovedCommands).
func (s Site) CommandApproved(command string) bool {
	for _, c := range s.ApprovedCommands {
		if c == command {
			return true
		}
	}
	return false
}

// HostCommandAllowed reports whether a project-supplied host command may run for
// a site without prompting. disabled is true when the global switch refuses all
// project host commands; otherwise allowed is true when the global skip is set or
// the user already approved this exact command for the site.
func HostCommandAllowed(siteName, command string) (allowed, disabled bool) {
	gcfg, _ := LoadGlobal()
	if gcfg.HostCommands.Disabled {
		return false, true
	}
	if gcfg.HostCommands.SkipConfirmation {
		return true, false
	}
	site, _ := FindSite(siteName)
	return site != nil && site.CommandApproved(command), false
}

// ApproveSiteCommand records that the user consented to run command on the host
// for the named site, so future runs and boot restore don't re-prompt. No-op if
// already recorded or the site is unknown.
func ApproveSiteCommand(siteName, command string) error {
	if command == "" {
		return nil
	}
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}
	for i := range reg.Sites {
		if reg.Sites[i].Name != siteName {
			continue
		}
		if reg.Sites[i].CommandApproved(command) {
			return nil
		}
		reg.Sites[i].ApprovedCommands = append(reg.Sites[i].ApprovedCommands, command)
		return SaveSites(reg)
	}
	return nil
}

// RemoveSite removes a site by name from the registry.
func RemoveSite(name string) error {
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}

	filtered := reg.Sites[:0]
	for _, s := range reg.Sites {
		if s.Name != name {
			filtered = append(filtered, s)
		}
	}
	reg.Sites = filtered
	return SaveSites(reg)
}

// ReorderSites permutes the registry so its sites follow the given order of
// names. Names that don't match a site are ignored, and any site whose name is
// absent from order is kept and appended after the ordered ones in its original
// relative position, so paused sites and grouped secondaries are never dropped.
func ReorderSites(order []string) error {
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}

	byName := make(map[string]int, len(reg.Sites))
	for i, s := range reg.Sites {
		byName[s.Name] = i
	}

	out := make([]Site, 0, len(reg.Sites))
	placed := make([]bool, len(reg.Sites))
	for _, name := range order {
		if i, ok := byName[name]; ok && !placed[i] {
			out = append(out, reg.Sites[i])
			placed[i] = true
		}
	}
	for i, s := range reg.Sites {
		if !placed[i] {
			out = append(out, s)
		}
	}

	reg.Sites = out
	return SaveSites(reg)
}

// IgnoreSite marks a site as ignored (used for parked sites that have been unlinked).
func IgnoreSite(name string) error {
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}

	for i, s := range reg.Sites {
		if s.Name == name {
			reg.Sites[i].Ignored = true
			return SaveSites(reg)
		}
	}
	return fmt.Errorf("site %q not found", name)
}

// SetSitePinned atomically updates just a site's pin flag. Like
// SetSiteIdleSuspendedWorkers it rewrites only that field under the write lock, so
// `servlo idle pin/unpin` can't clobber a concurrent SetSiteIdleSuspendedWorkers
// write the idle engine makes for the same site.
func SetSitePinned(name string, pinned bool) error {
	siteWriteMu.Lock()
	defer siteWriteMu.Unlock()
	reg, err := LoadSites()
	if err != nil {
		return err
	}
	for i := range reg.Sites {
		if reg.Sites[i].Name == name {
			reg.Sites[i].Pinned = pinned
			return SaveSites(reg)
		}
	}
	return fmt.Errorf("site %q not found", name)
}

// FindSite returns the site with the given name, or an error if not found.
func FindSite(name string) (*Site, error) {
	reg, err := LoadSites()
	if err != nil {
		return nil, err
	}

	for _, s := range reg.Sites {
		if s.Name == name {
			s := s
			return &s, nil
		}
	}
	return nil, fmt.Errorf("site %q not found", name)
}

// FindSiteByRef looks up a site by its internal name first, then by any of its
// domains, so a caller can pass either identifier.
func FindSiteByRef(ref string) (*Site, error) {
	if s, err := FindSite(ref); err == nil {
		return s, nil
	}
	return FindSiteByDomain(ref)
}

// ResolveSiteRef maps a site reference that may be a name or any of the site's
// domains to the canonical site name. An unknown reference is returned unchanged,
// so callers still surface their own "not found" error rather than a rewrite.
func ResolveSiteRef(ref string) string {
	if ref == "" {
		return ""
	}
	if s, err := FindSiteByRef(ref); err == nil {
		return s.Name
	}
	return ref
}

// FindSiteByPath returns the site whose path matches, or an error if not found.
func FindSiteByPath(path string) (*Site, error) {
	reg, err := LoadSites()
	if err != nil {
		return nil, err
	}

	target := CanonicalPath(path)
	for _, s := range reg.Sites {
		if CanonicalPath(s.Path) == target {
			s := s
			return &s, nil
		}
	}
	return nil, fmt.Errorf("site with path %q not found", path)
}

// CanonicalPath resolves symlinks in p so two spellings of the same directory
// compare equal. On ostree hosts /home is a symlink to /var/home, so os.Getwd
// can hand back either form for one project, which otherwise registers and
// lists twice (#930). Falls back to a cleaned path when the target can't be
// resolved, e.g. it no longer exists, so callers always get a usable value.
func CanonicalPath(p string) string {
	if p == "" {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

// FindSiteByDomain returns the site that has the given domain (checks all domains),
// or an error if not found.
func FindSiteByDomain(domain string) (*Site, error) {
	reg, err := LoadSites()
	if err != nil {
		return nil, err
	}

	for _, s := range reg.Sites {
		if s.HasDomain(domain) {
			s := s
			return &s, nil
		}
	}
	return nil, fmt.Errorf("site with domain %q not found", domain)
}

// IsDomainUsed checks if any site already uses this domain.
// Returns the site that uses it, or nil if the domain is free.
//
// The check is strict: a domain may only belong to one site, regardless of
// TLS scheme. Two sites cannot share the same domain even if one runs on
// HTTPS and the other on HTTP — DNS and browser caches don't reliably
// disambiguate by scheme, and the resulting setup is fragile.
func IsDomainUsed(domain string) (*Site, error) {
	reg, err := LoadSites()
	if err != nil {
		return nil, err
	}

	for _, s := range reg.Sites {
		if s.HasDomain(domain) {
			s := s
			return &s, nil
		}
	}
	return nil, nil
}

// Canonical host choices. A site names which of its two www forms is the real
// one; the other is permanently redirected to it.
const (
	CanonicalWWW  = "www"
	CanonicalApex = "apex"
)

// WWWPair returns a site's apex and www hosts when its domains are exactly a
// domain and its own www form, in either order. ok is false otherwise, which is
// every site the canonical-host toggle does not apply to.
//
// Exactly two, and one the www of the other: a site serving three names, or two
// unrelated ones, has no "the other host" to redirect, and a primary that is
// already a subdomain has no www form anyone wants.
func (s *Site) WWWPair() (apex, www string, ok bool) {
	if len(s.Domains) != 2 {
		return "", "", false
	}
	a, b := s.Domains[0], s.Domains[1]
	switch {
	case "www."+a == b:
		return a, b, true
	case "www."+b == a:
		return b, a, true
	}
	return "", "", false
}

// ValidateDeployExclude refuses an exclude path a deploy could not act on
// safely.
//
// These names decide where servlo writes files back during a deploy, so one
// that climbs out of the site is one that writes over the rest of the
// filesystem. An unset list and an empty one are both fine: they are the two
// ways of saying nothing about this site.
func (s *Site) ValidateDeployExclude() error {
	if s.DeployExclude == nil {
		return nil
	}
	for _, p := range *s.DeployExclude {
		if err := safeSitePath(p); err != nil {
			return fmt.Errorf("site %q deploy exclude: %w", s.Name, err)
		}
	}
	return nil
}

// ValidateCanonicalHost refuses a canonical host the site could not honour.
//
// The redirect it produces is permanent and browsers cache it, so pointing it
// at a name the site does not answer for is not a mistake an operator can undo
// by changing their mind: every visitor who saw it keeps going to the dead
// name until the cache expires.
func (s *Site) ValidateCanonicalHost() error {
	if s.CanonicalHost == "" {
		return nil
	}
	if s.CanonicalHost != CanonicalWWW && s.CanonicalHost != CanonicalApex {
		return fmt.Errorf("%q is not a canonical host: use %q, %q, or leave it unset to serve both", s.CanonicalHost, CanonicalWWW, CanonicalApex)
	}
	if _, _, ok := s.WWWPair(); !ok {
		return fmt.Errorf("a canonical host needs the site to serve a domain and its own www form, and this one serves %s", strings.Join(s.Domains, ", "))
	}
	return nil
}

// CanonicalRedirect returns the host to redirect and the host to redirect it
// to, or ok false when the site serves both.
func (s *Site) CanonicalRedirect() (from, to string, ok bool) {
	if s.ValidateCanonicalHost() != nil || s.CanonicalHost == "" {
		return "", "", false
	}
	apex, www, _ := s.WWWPair()
	if s.CanonicalHost == CanonicalWWW {
		return apex, www, true
	}
	return www, apex, true
}

// redirectPath is a path as a redirect rule may name one: absolute, and
// carrying nothing that would end the directive it lands in.
var redirectPath = regexp.MustCompile(`^/[^\s"'{};#\\]*$`)

// ValidateRedirects refuses a redirect the site could not serve or a browser
// could not escape.
//
// Both kinds land in an nginx directive and are followed by a browser, and a
// permanent one is cached, so a rule that loops or points somewhere useless is
// not a mistake the operator can simply undo: every visitor who saw it keeps
// following it until their cache expires.
func (s *Site) ValidateRedirects() error {
	if s.RedirectTo != "" {
		if err := s.validateWholeDomainTarget(); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, r := range s.Redirects {
		if !redirectPath.MatchString(r.From) {
			return fmt.Errorf("%q is not a path this site can redirect: use an absolute path like /old-page", r.From)
		}
		if seen[r.From] {
			return fmt.Errorf("%s is redirected twice, which is a duplicate location nginx will not load", r.From)
		}
		seen[r.From] = true
		if err := validateRedirectTarget(r.To); err != nil {
			return fmt.Errorf("the redirect for %s: %w", r.From, err)
		}
		if r.To == r.From {
			return fmt.Errorf("%s redirects to itself, which never arrives anywhere", r.From)
		}
	}
	return nil
}

// validateWholeDomainTarget refuses a destination that is not somewhere else.
func (s *Site) validateWholeDomainTarget() error {
	u, err := parseRedirectURL(s.RedirectTo)
	if err != nil {
		return fmt.Errorf("the whole-domain redirect target: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range s.Domains {
		if strings.EqualFold(d, host) {
			return fmt.Errorf("this site answers for %s, so redirecting the whole domain there is a loop the browser gives up on", host)
		}
	}
	return nil
}

// validateRedirectTarget accepts a path on this site or an absolute http(s)
// URL, and nothing else. A scheme a browser will execute rather than fetch has
// no business in a server-issued Location header.
func validateRedirectTarget(to string) error {
	if strings.HasPrefix(to, "/") {
		if !redirectPath.MatchString(to) {
			return fmt.Errorf("%q is not a usable path", to)
		}
		return nil
	}
	if _, err := parseRedirectURL(to); err != nil {
		return err
	}
	return nil
}

func parseRedirectURL(raw string) (*url.URL, error) {
	if strings.ContainsAny(raw, " \t\"'{};#\\\n\r\x00") {
		return nil, fmt.Errorf("%q contains a character that would end the directive it is written into", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%q is not a usable URL", raw)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q is not an absolute http or https URL", raw)
	}
	return u, nil
}
