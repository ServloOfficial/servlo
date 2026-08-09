package config

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// presetFileGenerators maps a preset FileMount's `generator:` name to the Go
// function that renders it at materialise time. This is the one part of a file
// mount that can't be static YAML: dynamic contents like pgAdmin's family-
// discovered servers.json. External presets reference these by name; shipping a
// genuinely new generator still requires a servlo release, the deliberate boundary
// that keeps store presets from carrying executable discovery logic.
var presetFileGenerators = map[string]func(*CustomService) (string, error){
	"pgadmin_servers": pgadminServersJSON,
	"pgadmin_pgpass":  pgadminPgpass,
	"db_ca_bundle":    dbCABundle,
}

// containerCABundle is where a generated CA bundle is mounted in an admin UI's
// container. A single file rather than one per connection, because a mount has
// a fixed target and a PEM bundle may hold as many certificates as it likes.
const containerCABundle = "/etc/servlo/db-ca-bundle.crt"

// DashboardProxyPrefix is the servlo-panel mount under which bundled admin
// dashboards (rabbitmq, redisinsight) are served same-origin so their cookies
// stay first-party in the iframe overlay. Shared by the servlo-panel proxy and the
// quadlet generator, which configures each upstream to serve its UI there.
const DashboardProxyPrefix = "/_svc/"

// DashboardProxyPath is the same-origin mount path for a proxied dashboard.
func DashboardProxyPath(name string) string {
	return DashboardProxyPrefix + name + "/"
}

// PresetProxyEnv returns the container env that makes a bundled upstream serve
// its UI under the same /_svc/<name> path the servlo-panel proxy mounts it at, so
// the dashboard embeds same-origin. It is injected at quadlet generation (not
// stored in the service YAML) so existing installs pick it up on the next
// start without a reinstall, mirroring how PresetFiles are re-sourced. Returns
// ok=false for presets that configure the prefix another way: rabbitmq uses a
// management.path_prefix conf mount (see presetFiles).
func PresetProxyEnv(svc *CustomService) (key, value string, ok bool) {
	if svc == nil {
		return "", "", false
	}
	switch svc.Preset {
	case "redisinsight":
		return "RI_PROXY_PATH", strings.TrimSuffix(DashboardProxyPath(svc.Name), "/"), true
	}
	return "", "", false
}

// PresetDashboardBootstrap returns an inline <script> to inject into the
// proxied dashboard's HTML so it opens already authenticated, mirroring how
// pgadmin/phpmyadmin auto-log-in via config. Returns "" when the dashboard
// needs no client-side priming.
//
// RabbitMQ's management UI (3.13) keeps no server session: the login form just
// stores HTTP Basic credentials in localStorage plus a `loggedIn` marker packed
// into its `m` cookie under a runtime-hashed key. We seed the same state before
// its scripts run. The cookie key is derived with the app's own hashCode/
// short_key algorithm at runtime (replicated inline) so it stays correct across
// versions; the page's CSP already allows unsafe-inline scripts.
func PresetDashboardBootstrap(svc *CustomService) string {
	if svc == nil {
		return ""
	}
	switch svc.Preset {
	case "rabbitmq":
		user := svc.Environment["RABBITMQ_DEFAULT_USER"]
		if user == "" {
			user = "root"
		}
		pass := svc.Environment["RABBITMQ_DEFAULT_PASS"]
		if pass == "" {
			// A service installed before the preset carried a password reads
			// its own; falling back to this install's generated one is the
			// only other value that can be right, and a literal here would put
			// a published password back into a browser's local storage.
			pass, _ = ServicePassword()
		}
		creds := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		return "<script>(function(){try{" +
			"if(localStorage.getItem('rabbitmq.credentials'))return;" +
			"function hc(s){var h=0;for(var i=0;i<s.length;i++){h=(31*h+s.charCodeAt(i))|0;}return h;}" +
			"localStorage.setItem('rabbitmq.credentials','" + creds + "');" +
			"localStorage.setItem('rabbitmq.auth-scheme','Basic');" +
			"document.cookie='m='+(Math.abs((hc('loggedIn')<<16)>>16).toString(16))+':true; path=/';" +
			"}catch(e){}})();</script>"
	}
	return ""
}

// PresetFiles returns the file mounts declared in the named preset's YAML, with
// each mount's `generator:` resolved to its ContentFn. It reads the preset fresh
// (embed bundle or store cache) so updating servlo, or the store definition, rolls
// out new file contents on the next service start without a reinstall. A mount
// naming an unknown generator is skipped rather than mounted empty, so a store
// preset built for a newer servlo degrades gracefully. Only presets carry files;
// custom services have any files: block stripped on load (see LoadCustomService).
func PresetFiles(presetName string) []FileMount {
	p, err := LoadPreset(presetName)
	if err != nil || len(p.Files) == 0 {
		return nil
	}
	out := make([]FileMount, 0, len(p.Files))
	for _, f := range p.Files {
		if f.Generator != "" {
			gen, ok := presetFileGenerators[f.Generator]
			if !ok {
				continue
			}
			f.ContentFn = gen
		}
		out = append(out, f)
	}
	return out
}

// pgadminFriendlyName turns a connection name like "postgres-18" or
// "do-managed" into a human-friendly server label.
func pgadminFriendlyName(name string) string {
	parts := strings.Split(strings.TrimPrefix(name, "servlo-"), "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return "Servlo " + strings.Join(parts, " ")
}

// postgresConnections is every Postgres database this install can reach.
//
// The fallback is the canonical local service, for a fresh install where
// nothing is running yet and for a config-package unit test with no seam wired:
// an empty servers.json is a pgAdmin that opens on "add a server", which is
// worse than one entry that might not be up yet.
func postgresConnections() []DBConnectionInfo {
	conns := databaseConnectionsFor("postgres")
	if len(conns) == 0 {
		return []DBConnectionInfo{{
			Name: "postgres", Family: "postgres", Local: true,
			Host: "servlo-postgres", Port: 5432, User: "postgres",
		}}
	}
	return conns
}

// pgadminSSLMode is what libpq calls the connection's protection. A local
// database is on the container network and is not asked to prove anything;
// verify-ca is checked against the bundle mounted beside servers.json.
func pgadminSSLMode(c DBConnectionInfo) string {
	switch {
	case c.Local:
		return "prefer"
	case c.TLSMode == "require":
		return "require"
	case c.TLSMode == "verify-ca":
		return "verify-ca"
	}
	return "prefer"
}

// pgadminServersJSON renders pgAdmin's servers.json with every Postgres
// database this install can reach: the local services and any managed server a
// site is on, each already logged in through the passfile beside it.
func pgadminServersJSON(_ *CustomService) (string, error) {
	type server struct {
		Name          string `json:"Name"`
		Group         string `json:"Group"`
		Host          string `json:"Host"`
		Port          int    `json:"Port"`
		MaintenanceDB string `json:"MaintenanceDB"`
		Username      string `json:"Username"`
		SSLMode       string `json:"SSLMode"`
		PassFile      string `json:"PassFile"`
		SSLRootCert   string `json:"SSLRootCert,omitempty"`
	}
	servers := map[string]server{}
	for i, c := range postgresConnections() {
		entry := server{
			Name:          pgadminFriendlyName(c.Name),
			Group:         "Servers",
			Host:          c.Host,
			Port:          c.Port,
			MaintenanceDB: "postgres",
			Username:      c.User,
			SSLMode:       pgadminSSLMode(c),
			PassFile:      "/pgpass",
		}
		if entry.SSLMode == "verify-ca" {
			entry.SSLRootCert = containerCABundle
		}
		servers[strconv.Itoa(i+1)] = entry
	}
	data, err := json.MarshalIndent(map[string]any{"Servers": servers}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// pgadminPgpass renders a libpq passfile with one line per Postgres database,
// so every server in the list opens without a password prompt. A password with
// a colon or a backslash in it is escaped, because a managed provider generates
// the administrator's password and servlo does not get to choose its alphabet.
func pgadminPgpass(_ *CustomService) (string, error) {
	var b strings.Builder
	for _, c := range postgresConnections() {
		password := c.Password
		if password == "" {
			// A connection whose password cannot be read still belongs in the
			// list; pgAdmin will ask for it rather than the entry vanishing.
			continue
		}
		b.WriteString(pgpassEscape(c.Host))
		b.WriteString(":" + strconv.Itoa(c.Port) + ":*:")
		b.WriteString(pgpassEscape(c.User))
		b.WriteString(":" + pgpassEscape(password) + "\n")
	}
	return b.String(), nil
}

// pgpassEscape escapes the two characters a passfile field cannot hold.
func pgpassEscape(v string) string {
	return strings.NewReplacer(`\`, `\\`, `:`, `\:`).Replace(v)
}

// dbCABundle concatenates the CA certificates of every managed database this
// install reaches, so an admin UI has one file to verify any of them against.
// Empty when nothing is verified, which is the ordinary case.
func dbCABundle(_ *CustomService) (string, error) {
	var b strings.Builder
	seen := map[string]bool{}
	for _, c := range databaseConnectionsFor("mysql,mariadb,postgres") {
		if c.TLSMode != "verify-ca" || c.CACert == "" || seen[c.CACert] {
			continue
		}
		seen[c.CACert] = true
		data, err := os.ReadFile(c.CACert)
		if err != nil {
			// A certificate that cannot be read leaves that one server
			// unverifiable rather than emptying the bundle for the rest.
			continue
		}
		b.Write(data)
		if !strings.HasSuffix(string(data), "\n") {
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}
