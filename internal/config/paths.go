package config

import (
	"os"
	"path/filepath"
)

func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func xdgDataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// ConfigDir returns ~/.config/servlo/ (or $XDG_CONFIG_HOME/servlo/).
func ConfigDir() string {
	return filepath.Join(xdgConfigHome(), "servlo")
}

// DataDir returns ~/.local/share/servlo/ (or $XDG_DATA_HOME/servlo/).
func DataDir() string {
	return filepath.Join(xdgDataHome(), "servlo")
}

// BinDir returns the servlo bin directory.
func BinDir() string {
	return filepath.Join(DataDir(), "bin")
}

// NodeGlobalDir is the npm prefix servlo points its node shim at, so
// `npm install -g foo` lands in a stable per-user path instead of a
// version-specific fnm directory that nothing has on PATH.
func NodeGlobalDir() string {
	return filepath.Join(DataDir(), "node-global")
}

// NginxDir returns the nginx data directory.
func NginxDir() string {
	return filepath.Join(DataDir(), "nginx")
}

// NginxConfD returns the nginx conf.d directory.
func NginxConfD() string {
	return filepath.Join(NginxDir(), "conf.d")
}

// NginxCustomD holds user-authored nginx snippets included at the end of
// each per-site server block. Servlo never writes here, so edits survive
// vhost regeneration and `servlo update`.
func NginxCustomD() string {
	return filepath.Join(NginxDir(), "custom.d")
}

// NginxCustomDBkp holds timestamped backups of per-site custom.d overrides
// produced by the web UI editor. It deliberately sits next to (not inside)
// custom.d/ because the generated vhost templates include
// /etc/nginx/custom.d/{domain}.conf*; a backup file in custom.d/ would be
// auto-loaded by nginx and produce duplicate directives.
func NginxCustomDBkp() string {
	return filepath.Join(NginxDir(), "custom.d.bkp")
}

// NginxConfDBkp holds the previous contents of a generated vhost, kept every
// time servlo replaces one. Outside conf.d, because nginx includes conf.d/*.conf
// and a backup kept beside the live file would load as a second server block
// for the same domain.
func NginxConfDBkp() string {
	return filepath.Join(NginxDir(), "conf.d.bkp")
}

// NginxHtpasswdDir holds one credential file per staging site, mounted
// read-only into the nginx container so auth_basic_user_file can reach it.
//
// Its own directory rather than a file beside the vhost: conf.d is included
// wholesale by nginx.conf, and a credential file sitting in it would be parsed
// as configuration the first time somebody widened the include glob.
func NginxHtpasswdDir() string {
	return filepath.Join(NginxDir(), "htpasswd")
}

// NginxHttpD holds user-authored nginx snippets included at the http{} level
// (e.g. global gzip, proxy buffers, client_max_body_size). Servlo never writes
// here, so edits survive nginx.conf regeneration and `servlo update`.
func NginxHttpD() string {
	return filepath.Join(NginxDir(), "http.d")
}

// NginxHttpUserConf is the single global http-level tuning override file. The
// zz- prefix sorts it after any other http.d snippets so user values win.
func NginxHttpUserConf() string {
	return filepath.Join(NginxHttpD(), "zz-servlo-user.conf")
}

// NginxHttpDBkp holds timestamped backups of the global http-level override
// produced by the web UI editor. It sits next to (not inside) http.d/ because
// nginx.conf includes /etc/nginx/http.d/*.conf; a backup inside http.d/ would
// be loaded too and produce duplicate http{} directives.
func NginxHttpDBkp() string {
	return filepath.Join(NginxDir(), "http.d.bkp")
}

// CertsDir returns the certs directory.
func CertsDir() string {
	return filepath.Join(DataDir(), "certs")
}

// SiteBackupsDir is where a site's own backups land on this server before they
// go anywhere else.
//
// A local copy exists even when a remote destination is configured, because the
// restore an operator needs most urgently is usually the one taken an hour ago,
// and fetching it back from object storage to find that out is time spent while
// the site is down.
//
// Deliberately not BackupsDir, which is the migration dumps below and predates
// this. Sharing a directory would put two unrelated things under one retention
// sweep, and the first one to run would delete the other's.
func SiteBackupsDir() string {
	return filepath.Join(DataDir(), "site-backups")
}

// ACMEChallengeDir returns the webroot nginx serves HTTP-01 challenges from.
// One directory for every site rather than one per site: the challenge is a
// token nginx hands back verbatim, it carries no site content, and a single
// bind mount is one thing to get right instead of one per vhost.
func ACMEChallengeDir() string {
	return filepath.Join(DataDir(), "acme-challenge")
}

// ACMEAccountDir returns where the ACME account key and registration live.
// Namespaced by directory host so a staging account and a production account
// never share a key: an ACME account belongs to the directory that issued it,
// and reusing one against the other fails registration in a way that reads
// like a broken key.
func ACMEAccountDir(directoryHost string) string {
	return filepath.Join(DataDir(), "acme", directoryHost)
}

// DataSubDir returns a named subdirectory under data.
func DataSubDir(name string) string {
	return filepath.Join(DataDir(), "data", name)
}

// BackupsDir returns the directory where migration dumps are stored so users
// can recover manually if an automated migration fails.
func BackupsDir() string {
	return filepath.Join(DataDir(), "backups")
}

// SnapshotsDir returns the directory where db:snapshot point-in-time database
// copies are stored, organised per service and database scope.
func SnapshotsDir() string {
	return filepath.Join(DataDir(), "snapshots")
}

// SitesFile returns the path to sites.yaml.
func SitesFile() string {
	return filepath.Join(DataDir(), "sites.yaml")
}

// GlobalConfigFile returns the path to config.yaml.
func GlobalConfigFile() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// QuadletDir returns the Podman quadlet directory.
func QuadletDir() string {
	return filepath.Join(xdgConfigHome(), "containers", "systemd")
}

// SystemdUserDir returns the systemd user unit directory.
func SystemdUserDir() string {
	return filepath.Join(xdgConfigHome(), "systemd", "user")
}

// PHPImageHashFile returns the path to the stored PHP-FPM Containerfile hash.
func PHPImageHashFile() string {
	return filepath.Join(DataDir(), "php-image-hash")
}

// PHPUserIniFile returns the host path for the per-version user php.ini file.
func PHPUserIniFile(version string) string {
	return filepath.Join(DataDir(), "php", version, "98-user.ini")
}

// SharedIniFile returns the host path for the version-agnostic shared php.ini.
// A single copy is bind-mounted into every PHP container below the per-version
// 98-user.ini, so a setting placed here applies to all versions while any
// per-version file still overrides it (conf.d loads alphabetically, last wins).
func SharedIniFile() string {
	return filepath.Join(DataDir(), "php", "shared", "95-shared.ini")
}

// SharedIniBkpDir holds timestamped backups of the shared ini produced by the
// editor, next to (not inside) the shared dir so no FPM container's conf.d scan
// loads a backup as live config.
func SharedIniBkpDir() string {
	return filepath.Join(DataDir(), "php", "shared", "ini.bkp")
}

// SitePHPUserIniFile is the per-site user php.ini for a runtime site that runs
// its own container (FrankenPHP). Unlike PHPUserIniFile (shared by every site on
// a PHP version), this is scoped to one site so its php.ini is independent.
func SitePHPUserIniFile(siteName string) string {
	return filepath.Join(DataDir(), "php", "sites", siteName, "98-user.ini")
}

// SitePHPUserIniBkpDir holds timestamped backups of a site's per-site user ini,
// next to (not inside) the file so the container's conf.d scan never loads them.
func SitePHPUserIniBkpDir(siteName string) string {
	return filepath.Join(DataDir(), "php", "sites", siteName, "ini.bkp")
}

// PHPUserIniBkpDir holds timestamped backups of the per-version user ini
// produced by the web UI editor. It sits next to (not inside) the version
// directory's ini scan path so the FPM container does not load backup files
// as live config.
func PHPUserIniBkpDir(version string) string {
	return filepath.Join(DataDir(), "php", version, "ini.bkp")
}

// CustomServicesDir returns the directory for custom service YAML files.
func CustomServicesDir() string {
	return filepath.Join(ConfigDir(), "services")
}

// ServiceFilesDir returns the directory holding rendered FileMount content
// for the named custom service. Each file is bind-mounted into the container
// at its declared target path.
func ServiceFilesDir(name string) string {
	return filepath.Join(DataDir(), "service-files", name)
}

// ServiceTuningFile returns the host path for a service's user-editable runtime
// tuning override. Servlo seeds it once with a commented template and never
// overwrites it afterwards, so edits survive `servlo service reinstall` and
// `servlo update` — the same never-clobber contract as NginxCustomD and the
// per-version PHP 98-user.ini.
func ServiceTuningFile(name string) string {
	return filepath.Join(DataDir(), "service-tuning", name+".conf")
}

// ServiceTuningAuxFile returns the host path for a service's servlo-managed
// tuning helper file — a static config that the family's tuning Command depends
// on (e.g. the postgres `config_file` wrapper that `include_dir`s the user
// override directory, because `-c include_dir` is rejected at runtime). Unlike
// ServiceTuningFile this is regenerated on every start, never user-edited, and
// lives alongside the override with a distinct `.aux.conf` suffix so it is never
// mistaken for it.
func ServiceTuningAuxFile(name string) string {
	return filepath.Join(DataDir(), "service-tuning", name+".aux.conf")
}

// ServiceTuningBkpDir holds timestamped backups of per-service tuning
// overrides produced when the user ticks "back up the current file first"
// before saving in the web UI editor. It lives next to (not inside) the
// service-tuning/ directory so it cannot be picked up by any future
// include glob and never gets bind-mounted into the service container,
// keeping backups invisible to the running service even if it tries to
// scan its config dir.
func ServiceTuningBkpDir() string {
	return filepath.Join(DataDir(), "service-tuning.bkp")
}

// FrameworksDir returns the directory for user-defined framework YAML files.
func FrameworksDir() string {
	return filepath.Join(ConfigDir(), "frameworks")
}

// StoreFrameworksDir returns the directory for store-installed framework YAML files.
func StoreFrameworksDir() string {
	return filepath.Join(DataDir(), "frameworks")
}

// StoreIndexFile returns the path to the locally cached framework store index.
// The store package refreshes it in the background; offline detection and
// listing read it so a fresh machine can resolve any framework without a
// definition already on disk.
func StoreIndexFile() string {
	return filepath.Join(StoreFrameworksDir(), "index.json")
}

// StorePresetsDir returns the directory for store-installed service-preset YAML
// files, fetched from the external service store. It sits under the preset-source
// seam as a layer above the embedded bundle: a valid preset here is served in
// place of (or in addition to) the built-in of the same name, while the embed
// bundle stays as the permanent offline fallback. Distinct from ConfigDir()/services
// (user-defined custom services) so store presets and user services never mix.
func StorePresetsDir() string {
	return filepath.Join(DataDir(), "service-presets")
}

// UpdateCheckFile returns the path to the cached update-check state file.
func UpdateCheckFile() string {
	return filepath.Join(DataDir(), "update-check.json")
}

// BackupBinaryFile returns the path to the backup servlo binary used for rollback.
func BackupBinaryFile() string {
	return filepath.Join(DataDir(), "servlo.bak")
}

// BackupVersionFile returns the path to the file storing the pre-update version string.
func BackupVersionFile() string {
	return filepath.Join(DataDir(), "rollback-version")
}

// PausedDir returns the directory where paused-site landing page HTML files are stored.
func PausedDir() string {
	return filepath.Join(DataDir(), "paused")
}

// ErrorPagesDir returns the directory where nginx error page HTML files are stored.
func ErrorPagesDir() string {
	return filepath.Join(DataDir(), "error-pages")
}

// RunDir returns the directory for runtime sockets shared between servlo-panel
// (host process) and servlo-nginx (container). Bind-mounted into servlo-nginx so
// the servlo.localhost vhost can reach servlo-panel without depending on container
// → host TCP routing (host.containers.internal / 169.254.1.2), which is
// unreliable across podman/netavark/pasta versions and host network changes.
func RunDir() string {
	return filepath.Join(DataDir(), "run")
}

// UISocketPath returns the path to the servlo-panel unix domain socket.
func UISocketPath() string {
	return filepath.Join(RunDir(), "servlo-panel.sock")
}

// UIClientNetwork / UIClientAddr give the transport a CLI process uses to reach
// the running servlo-panel daemon. On macOS the unix socket is never created (the
// server binds it Linux-only, see internal/ui/server.go), so the CLI dials the
// same TCP loopback the dashboard uses; on Linux it stays on the unix socket.
// Mirrors the DumpsListenNetwork/Addr split. The port matches servlo-panel's fixed
// listen port (internal/ui/server.go listenAddr).
func UIClientNetwork() string {
	return "unix"
}

// UIClientAddr is the address paired with UIClientNetwork.
func UIClientAddr() string {
	return UISocketPath()
}

// IdleActivityFile is where the servlo-watcher persists per-site last-active times
// so a restart restores the idle countdowns instead of re-seeding to now; servlo-panel
// and the CLI read it to render each site's idle state. Lives in RunDir.
func IdleActivityFile() string {
	return filepath.Join(RunDir(), "idle-activity.json")
}

// RequestStatsFile is where the watcher persists its rolling per-site request
// timing snapshot for servlo-panel to read, since the two run as separate processes
// and only the watcher binds the nginx access feed. Ephemeral, lives in RunDir.
func RequestStatsFile() string {
	return filepath.Join(RunDir(), "request-stats.json")
}

// RequestStatsDB is the durable SQLite store of individual requests the watcher
// writes and servlo-panel reads to build the request-timing analytics view over any
// window. Unlike the ephemeral snapshot it lives in DataDir so history survives
// a reboot.
func RequestStatsDB() string {
	return filepath.Join(DataDir(), "request-stats.db")
}

// AccessSocketPath is the unix datagram socket the servlo-watcher binds to receive
// the nginx access feed (one "$host" line per request) that drives's
// per-site last-active tracking. It lives in RunDir, which is bind-mounted into
// the servlo-nginx container at the same path, so nginx's syslog access_log can
// reach it without container→host TCP routing.
func AccessSocketPath() string {
	return filepath.Join(RunDir(), "servlo-access.sock")
}

// AccessFeedUDPPort is the UDP port the watcher binds on darwin for the nginx
// access feed: nginx runs in the podman-machine VM where the host unix socket
// isn't reachable, so the feed travels over gvproxy UDP (like DumpsTCPPort).
const AccessFeedUDPPort = "9914"

// AccessFeedListenAddr is the host address the watcher binds for the darwin UDP
// access feed. Loopback matches the gvproxy host.containers.internal forward.
func AccessFeedListenAddr() string {
	return "127.0.0.1:" + AccessFeedUDPPort
}

// AccessLogTarget is the nginx syslog `server=` for the access feed: the
// bind-mounted unix socket on Linux, or host.containers.internal over gvproxy
// UDP on macOS where nginx lives in the VM and the host socket isn't reachable.
func AccessLogTarget() string {
	return "unix:" + AccessSocketPath()
}

// ControlSocketPath is the unix datagram socket the servlo-watcher binds for
// control messages from the CLI and dashboard
// toggle, and "activity <site>" from the CLI shims.
func ControlSocketPath() string {
	return filepath.Join(RunDir(), "servlo-idle-control.sock")
}

// stoppedMarkerPath is the sentinel `servlo stop` writes and `servlo start` clears.
// It lets long-running loops (the worker health watcher, heal notifications)
// tell an intentional shutdown from worker drift.
func stoppedMarkerPath() string {
	return filepath.Join(RunDir(), "stopped")
}

// MarkStopped records that servlo was intentionally stopped, so background
// watchers suppress worker heal/notification noise until the next start.
func MarkStopped() error {
	if err := os.MkdirAll(RunDir(), 0755); err != nil {
		return err
	}
	guardRealWrite(stoppedMarkerPath())
	return os.WriteFile(stoppedMarkerPath(), []byte("stopped\n"), 0644)
}

// ClearStopped clears the intentional-stop marker (servlo is starting or running).
func ClearStopped() error {
	if err := os.Remove(stoppedMarkerPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsStopped reports whether servlo was intentionally stopped via `servlo stop`.
func IsStopped() bool {
	_, err := os.Stat(stoppedMarkerPath())
	return err == nil
}

// PprofMarkerPath is the sentinel that unlocks servlo-panel's profiling endpoints.
// Exported so the CLI and docs can name the exact file a user has to create.
func PprofMarkerPath() string {
	return filepath.Join(RunDir(), "pprof.enabled")
}

// PprofEnabled reports whether profiling has been unlocked. Deliberately read
// per request rather than at startup: a daemon that is burning CPU right now
// has to be profilable without a restart, since restarting it discards the
// very state worth capturing.
func PprofEnabled() bool {
	_, err := os.Stat(PprofMarkerPath())
	return err == nil
}

// ContainerHostsFile returns the path to the shared hosts file mounted into PHP containers.
func ContainerHostsFile() string {
	return filepath.Join(DataDir(), "hosts")
}

// BrowserHostsFile returns the path to the hosts file mounted into containers
// that declare ShareHosts. It maps every registered site domain to the nginx
// container's IP so a container can reach servlo sites directly over the
// Podman network instead of going through the host gateway.
func BrowserHostsFile() string {
	return filepath.Join(DataDir(), "browser-hosts")
}

// FPMPoolRoot returns the parent of every FPM container's pool directory.
func FPMPoolRoot() string {
	return filepath.Join(DataDir(), "fpm-pools")
}

// FPMPoolDir returns the directory holding one PHP-FPM pool per site for a
// single FPM container, named for that container's unit. It is bind-mounted
// into that container as php-fpm.d, so a file here is a pool its master process
// reads on its next reload.
//
// Per container rather than one directory for all of them, because a master
// defines every pool it can see and binds every socket those pools listen on.
// Two masters sharing a directory would each define the other's sites, and
// whichever started last would be answering for all of them: a site pinned to
// 8.3 quietly served by 8.4.
func FPMPoolDir(unit string) string {
	return filepath.Join(FPMPoolRoot(), unit)
}

// FPMSocketDir returns the directory a site's pool listens in. It sits under
// RunDir because that is what both the FPM container and servlo-nginx mount at
// the same absolute path, which is what lets the pool's listen address and the
// vhost's fastcgi_pass name the same socket.
func FPMSocketDir() string {
	return filepath.Join(RunDir(), "fpm")
}
