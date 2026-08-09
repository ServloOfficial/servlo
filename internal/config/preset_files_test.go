package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 6 parity: an external store preset can ship its own static file mount
// purely in YAML, no Go change required.
func TestPresetFiles_ExternalStorePresetShipsFiles(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeStorePreset(t, "ext-svc", "name: ext-svc\nimage: example/ext:1\nfiles:\n  - target: /etc/ext.conf\n    content: |\n      hello = world\n")
	files := PresetFiles("ext-svc")
	if len(files) != 1 || files[0].Target != "/etc/ext.conf" {
		t.Fatalf("external preset files = %+v", files)
	}
	if !strings.Contains(files[0].Content, "hello = world") {
		t.Errorf("content = %q, want it to contain the mounted body", files[0].Content)
	}
}

// A mount naming an unknown generator (e.g. a store preset built for a newer
// servlo) is skipped, never mounted empty.
func TestPresetFiles_UnknownGeneratorSkipped(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeStorePreset(t, "gen-svc", "name: gen-svc\nimage: example/gen:1\nfiles:\n  - target: /a\n    content: static\n  - target: /b\n    generator: does-not-exist\n")
	files := PresetFiles("gen-svc")
	if len(files) != 1 || files[0].Target != "/a" {
		t.Errorf("unknown generator must be skipped, got %+v", files)
	}
}

// A known generator name resolves to its Go ContentFn.
func TestPresetFiles_KnownGeneratorResolves(t *testing.T) {
	found := false
	for _, f := range PresetFiles("pgadmin") {
		if f.Target == "/pgadmin4/servers.json" {
			found = true
			if f.ContentFn == nil {
				t.Error("pgadmin_servers generator did not resolve to a ContentFn")
			}
		}
	}
	if !found {
		t.Error("pgadmin servers.json mount missing")
	}
}

// stubConnections points the generators at a fixed set of databases, standing
// in for the enumeration serviceops wires in.
func stubConnections(t *testing.T, conns ...DBConnectionInfo) {
	t.Helper()
	old := DatabaseConnections
	DatabaseConnections = func([]string) []DBConnectionInfo { return conns }
	t.Cleanup(func() { DatabaseConnections = old })
}

func localPostgres(name string) DBConnectionInfo {
	return DBConnectionInfo{
		Name: name, Family: "postgres", Local: true,
		Host: "servlo-" + name, Port: 5432, User: "postgres", Password: "generated-install-password",
	}
}

// A site's database may be on a managed server, and pgAdmin is no use to that
// site if the only servers it lists are the containers on this box.
func TestPgadminServersJSON_listsEveryDatabaseIncludingManaged(t *testing.T) {
	stubConnections(t,
		localPostgres("postgres"),
		localPostgres("postgres-18"),
		DBConnectionInfo{
			Name: "do-managed", Family: "postgres",
			Host: "db.example.net", Port: 25060, User: "doadmin", Password: "provider-password",
			TLSMode: "verify-ca", CACert: "/home/dev/.config/servlo/db-ca/do-managed.crt",
		},
	)

	out, err := pgadminServersJSON(nil)
	if err != nil {
		t.Fatalf("pgadminServersJSON: %v", err)
	}
	for _, want := range []string{
		`"Host": "servlo-postgres"`,
		`"Host": "servlo-postgres-18"`,
		`"Name": "Servlo Postgres 18"`,
		`"Host": "db.example.net"`,
		`"Port": 25060`,
		`"Username": "doadmin"`,
		`"SSLMode": "verify-ca"`,
		`"SSLRootCert": "/etc/servlo/db-ca-bundle.crt"`,
		`"PassFile": "/pgpass"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("servers.json missing %q\n%s", want, out)
		}
	}
	// A local database is on the container network and is not asked to prove
	// anything; requiring TLS there would break every existing install.
	if !strings.Contains(out, `"SSLMode": "prefer"`) {
		t.Errorf("a local server must stay on prefer\n%s", out)
	}
}

// The passfile carries this install's real password. It shipped a literal
// "servlo" long after the presets moved to a generated one, which is a pgAdmin
// that asks for a password nobody has.
func TestPgadminPgpass_carriesEachConnectionsOwnPassword(t *testing.T) {
	stubConnections(t,
		localPostgres("postgres"),
		DBConnectionInfo{Name: "do-managed", Family: "postgres", Host: "db.example.net", Port: 25060, User: "doadmin", Password: "provider:password"},
	)

	out, err := pgadminPgpass(nil)
	if err != nil {
		t.Fatalf("pgadminPgpass: %v", err)
	}
	if !strings.Contains(out, "servlo-postgres:5432:*:postgres:generated-install-password") {
		t.Errorf("pgpass missing the local line\n%s", out)
	}
	// A provider generates the administrator's password, so a colon in it is
	// somebody else's decision and has to survive the file format.
	if !strings.Contains(out, `db.example.net:25060:*:doadmin:provider\:password`) {
		t.Errorf("pgpass did not escape the managed password\n%s", out)
	}
	if strings.Contains(out, ":servlo\n") {
		t.Errorf("pgpass still carries the published literal password\n%s", out)
	}
}

// The bundle is what a verify-ca server is checked against, and it holds every
// such certificate because one mount cannot be one file per connection.
func TestDBCABundle_concatenatesTheVerifiedCertificates(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "one.crt")
	second := filepath.Join(dir, "two.crt")
	if err := os.WriteFile(first, []byte("-----BEGIN CERTIFICATE-----\nONE\n-----END CERTIFICATE-----"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("-----BEGIN CERTIFICATE-----\nTWO\n-----END CERTIFICATE-----\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stubConnections(t,
		localPostgres("postgres"),
		DBConnectionInfo{Name: "a", Family: "mysql", Host: "a.example", Port: 25060, TLSMode: "verify-ca", CACert: first},
		DBConnectionInfo{Name: "b", Family: "postgres", Host: "b.example", Port: 25060, TLSMode: "require"},
		DBConnectionInfo{Name: "c", Family: "postgres", Host: "c.example", Port: 25060, TLSMode: "verify-ca", CACert: second},
	)

	out, err := dbCABundle(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ONE") || !strings.Contains(out, "TWO") {
		t.Errorf("bundle is missing a certificate:\n%s", out)
	}
	// A certificate that does not end in a newline must not run into the next
	// one's BEGIN line, or neither parses.
	if strings.Contains(out, "-----END CERTIFICATE----------BEGIN") {
		t.Errorf("two certificates ran together:\n%s", out)
	}
}

func TestPgadminPreset_consumesPostgresFamily(t *testing.T) {
	// dynamic_env wires pgadmin into the family-consumer regeneration path,
	// so installing/removing a postgres alternate triggers a servers.json
	// rebuild and a pgadmin restart.
	p, err := LoadPreset("pgadmin")
	if err != nil {
		t.Fatalf("LoadPreset(pgadmin): %v", err)
	}
	if got := p.DynamicEnv["SERVLO_POSTGRES_HOSTS"]; got != "connections:postgres=hostport" {
		t.Errorf("pgadmin must read the postgres connections, got %q", got)
	}
	if p.Environment["PGADMIN_REPLACE_SERVERS_ON_STARTUP"] != "True" {
		t.Errorf("pgadmin must set PGADMIN_REPLACE_SERVERS_ON_STARTUP=True so the regenerated servers.json gets re-imported on restart")
	}
}

func TestRabbitMQPresetMountsPathPrefix(t *testing.T) {
	files := PresetFiles("rabbitmq")
	if len(files) == 0 {
		t.Fatal("rabbitmq preset has no file mounts")
	}
	f := files[0]
	if f.Target != "/etc/rabbitmq/conf.d/10-servlo-path-prefix.conf" {
		t.Errorf("rabbitmq conf mounted at %q, want /etc/rabbitmq/conf.d/10-servlo-path-prefix.conf", f.Target)
	}
	// The management UI must serve under the same prefix the servlo-panel proxy
	// mounts it at, or the iframe loads a blank shell (absolute asset paths).
	if !strings.Contains(f.Content, "management.path_prefix = /_svc/rabbitmq") {
		t.Errorf("rabbitmq conf missing management.path_prefix = /_svc/rabbitmq\n%s", f.Content)
	}
}

func TestRedisInsightProxyEnvInjectedByPreset(t *testing.T) {
	// RI_PROXY_PATH is injected at quadlet generation from the preset, not
	// stored in the service YAML, so existing installs serve under the proxy
	// mount after a restart without a reinstall.
	svc := &CustomService{Name: "redisinsight", Preset: "redisinsight", Dashboard: "http://localhost:8085", DashboardExternal: true}
	k, v, ok := PresetProxyEnv(svc)
	if !ok || k != "RI_PROXY_PATH" || v != "/_svc/redisinsight" {
		t.Errorf("PresetProxyEnv = (%q,%q,%v), want (RI_PROXY_PATH, /_svc/redisinsight, true)", k, v, ok)
	}
	// A user custom service (no bundled preset) gets no proxy env.
	if _, _, ok := PresetProxyEnv(&CustomService{Name: "x"}); ok {
		t.Error("non-bundled service must not receive proxy env")
	}
}

func TestRabbitMQDashboardBootstrap_seedsBasicAuth(t *testing.T) {
	svc := &CustomService{
		Name:      "rabbitmq",
		Preset:    "rabbitmq",
		Dashboard: "http://localhost:15672",
		Environment: map[string]string{
			"RABBITMQ_DEFAULT_USER": "root",
			"RABBITMQ_DEFAULT_PASS": "servlo",
		},
	}
	s := PresetDashboardBootstrap(svc)
	// base64("root:servlo") == "cm9vdDpzZXJ2bG8="
	for _, want := range []string{"<script>", "rabbitmq.credentials", "cm9vdDpzZXJ2bG8=", "rabbitmq.auth-scheme", "loggedIn"} {
		if !strings.Contains(s, want) {
			t.Errorf("rabbitmq bootstrap missing %q:\n%s", want, s)
		}
	}
	// A user custom service (no bundled preset) gets no bootstrap.
	if PresetDashboardBootstrap(&CustomService{Name: "x"}) != "" {
		t.Error("non-bundled service must not get a dashboard bootstrap")
	}
}

func TestMySQLPresetContainsCompatDirectives(t *testing.T) {
	files := PresetFiles("mysql")
	if len(files) == 0 {
		t.Fatal("mysql preset has no file mounts")
	}

	cnf := files[0].Content

	for _, directive := range []string{
		"mysql-native-password=ON",
		"restrict-fk-on-non-standard-key=OFF",
	} {
		if !strings.Contains(cnf, directive) {
			t.Errorf("mysql servlo.cnf missing %q", directive)
		}
	}
	// mysql 9.x removed mysql_native_password, so the policy line must not
	// pin it as the primary or the server refuses to initialise.
	if strings.Contains(cnf, "authentication_policy=") {
		t.Errorf("mysql servlo.cnf must not pin authentication_policy: it breaks mysql 9.x init")
	}
}

// Removed in MySQL 8.0; kept silent on 5.7/8.x via the loose- prefix but
// generated a startup warning on every container start. servlo no longer
// ships 5.6, so they should not be re-added.
func TestMySQLPresetExcludesRemovedDirectives(t *testing.T) {
	files := PresetFiles("mysql")
	if len(files) == 0 {
		t.Fatal("mysql preset has no file mounts")
	}

	cnf := files[0].Content

	for _, directive := range []string{
		"innodb_large_prefix",
		"innodb_file_format",
	} {
		if strings.Contains(cnf, directive) {
			t.Errorf("mysql servlo.cnf still contains removed-in-8.0 directive %q", directive)
		}
	}
}
