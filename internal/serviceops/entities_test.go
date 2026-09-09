package serviceops

import (
	"github.com/ServloOfficial/servlo/internal/config"
	"strings"
	"testing"
)

const entityEngineYAML = `name: myengine
image: example/engine:1
introspect:
  entities:
    - kind: buckets
      list: list-buckets
      columns:
        - key: objects
          format: number
        - key: size
          format: bytes
      actions:
        create: make-bucket {{name}}
        delete:
          exec: remove-bucket {{name}}
          destructive: true
`

func TestEntityForResolvesDeclaredKind(t *testing.T) {
	writeCustomService(t, "myengine", entityEngineYAML)
	spec := EntityFor("myengine", "buckets")
	if spec == nil {
		t.Fatal("declared buckets entity did not resolve")
	}
	if spec.List != "list-buckets" || len(spec.Columns) != 2 {
		t.Errorf("spec = %+v", spec)
	}
	if EntityFor("myengine", "queues") != nil {
		t.Error("undeclared kind must resolve to nil")
	}
}

func TestEntityForDatabasesFallsBackToLegacyField(t *testing.T) {
	writeCustomService(t, "myengine", "name: myengine\nimage: example/engine:1\nintrospect:\n  list_databases: echo hi\n")
	spec := EntityFor("myengine", "databases")
	if spec == nil || spec.List != "echo hi" {
		t.Fatalf("legacy list_databases must synthesize a databases entity, got %+v", spec)
	}
	if len(spec.Actions) != 0 {
		t.Errorf("synthesized spec must carry no actions: %+v", spec.Actions)
	}
}

func TestEntityForPrefersPresetOverBareStoredDefinition(t *testing.T) {
	// An engine installed before entities existed resolves them from the preset
	// it came from, the same way IntrospectCommand always has.
	writeCustomService(t, "mysql", "name: mysql\nimage: docker.io/library/mysql:8\npreset: mysql\n")
	spec := EntityFor("mysql", "databases")
	if spec == nil || spec.List == "" {
		t.Fatal("want the bundled mysql preset entity, got none")
	}
}

// The databases kind has its own tab, so the generic overview must not repeat
// it alongside the other kinds a service declares.
func TestServiceEntitiesExcludesDatabases(t *testing.T) {
	writeCustomService(t, "myengine", `name: myengine
image: example/engine:1
introspect:
  entities:
    - kind: databases
      list: list-dbs
    - kind: buckets
      list: list-buckets
`)
	kinds := EntityKinds("myengine")
	if len(kinds) != 1 || kinds[0] != "buckets" {
		t.Fatalf("kinds = %v, want [buckets]", kinds)
	}
	if EntityKinds("mysql") != nil {
		t.Error("an engine declaring only databases must expose no generic kinds")
	}
}

// The generic entity surface is a list with actions hanging off each row. A
// kind that declares nothing to list is not that: it is a set of statements
// something else drives (site_users, whose surface is the site's own database
// card), and offering it as an empty table with buttons would be both clutter
// and a route that runs a command with half its placeholders unfilled.
func TestServiceEntitiesExcludesKindsWithNothingToList(t *testing.T) {
	writeCustomService(t, "myengine", `name: myengine
image: example/engine:1
introspect:
  entities:
    - kind: buckets
      list: list-buckets
    - kind: site_users
      actions:
        create: make-user {{name}}
`)
	kinds := EntityKinds("myengine")
	if len(kinds) != 1 || kinds[0] != "buckets" {
		t.Fatalf("kinds = %v, want [buckets]", kinds)
	}
	// It still resolves for the code that does drive it.
	if EntityFor("myengine", "site_users") == nil {
		t.Error("a kind kept off the generic surface must still resolve for its own caller")
	}
}

func TestExpandEntityCommandValidatesAndSubstitutes(t *testing.T) {
	cmd, err := expandEntityCommand("do-thing {{name}} on {{name}}", "shop_db")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "do-thing shop_db on shop_db" {
		t.Errorf("expanded = %q", cmd)
	}
	// The name pattern is the injection guard: anything outside it never
	// reaches the shell.
	for _, bad := range []string{"", "a;rm -rf /", "x`y`", "a b", "$(x)", "-"} {
		if _, err := expandEntityCommand("do {{name}}", bad); err == nil {
			t.Errorf("name %q must be rejected", bad)
		}
	}
}

func TestParseEntityRows(t *testing.T) {
	rows := parseEntityRows([]byte("alpha\t3\t1024\nbeta\t0\t0\n\n"))
	if len(rows) != 2 {
		t.Fatalf("got %d rows: %+v", len(rows), rows)
	}
	if rows[0].Name != "alpha" || len(rows[0].Values) != 2 || rows[0].Values[1] != "1024" {
		t.Errorf("row 0 = %+v", rows[0])
	}
}

// An image-less entity execs inside the service container with the fixed
// admin credentials; one with a client image runs ephemerally on the servlo
// network with only its own env, and the image's entrypoint is overridden
// because client images make their tool the entrypoint.
//
// Either way the credentials are named in the argv and spelled in the
// environment, never the other way round. See the password test below.
func TestEntityCommandArgs(t *testing.T) {
	execArgs, execEnv := entityCommandArgs("mysql", "", nil, "list-cmd", false)
	joined := strings.Join(execArgs, " ")
	if execArgs[0] != "exec" || !strings.Contains(joined, "servlo-mysql sh -c list-cmd") {
		t.Errorf("exec args = %v", execArgs)
	}
	if !strings.Contains(joined, "--env MYSQL_PWD") {
		t.Errorf("exec args do not forward the admin credential by name: %v", execArgs)
	}
	if !hasPrefixIn(execEnv, "MYSQL_PWD=") {
		t.Errorf("the admin credential is not in the env for the process to carry: %v", redactEnvNames(execEnv))
	}

	runArgs, runEnv := entityCommandArgs("rustfs", "docker.io/rclone/rclone:latest", []string{"A=b"}, "tar-cmd", true)
	joined = strings.Join(runArgs, " ")
	for _, want := range []string{"run --rm -i", "--network servlo", "--entrypoint sh", "-e A", "docker.io/rclone/rclone:latest -c tar-cmd"} {
		if !strings.Contains(joined, want) {
			t.Errorf("run args missing %q: %v", want, runArgs)
		}
	}
	if !hasPrefixIn(runEnv, "A=b") {
		t.Errorf("the declared env is not carried: %v", runEnv)
	}
	if hasPrefixIn(runEnv, "MYSQL_PWD=") {
		t.Errorf("a client run must not carry the exec admin env: %v", redactEnvNames(runEnv))
	}
}

func hasPrefixIn(list []string, prefix string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// redactEnvNames prints what an env list holds without printing the values,
// because a failure here is about a credential and a test log is a file.
func redactEnvNames(env []string) []string {
	names := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		names = append(names, name)
	}
	return names
}

// The install's database password must never be in an argv.
//
// /proc/<pid>/cmdline is readable by every process on the machine, and on this
// one every site runs as the same Linux user (PRD section 6), so a password
// spelled into a podman argument is a password every site can read while the
// command runs. Listing databases happens on a panel page load, so the window
// is not rare.
//
// internal/dbexec already does this correctly and says why: podman reads
// `--env NAME` out of its own environment, so the value travels in this
// process rather than in an argument list. This is the same rule for the
// entity commands.
func TestEntityCommandArgs_NeverSpellTheAdminPasswordInTheArgv(t *testing.T) {
	password, err := config.ServicePassword()
	if err != nil {
		t.Fatalf("reading the install's service password: %v", err)
	}
	if password == "" {
		t.Fatal("no service password, so this test would pass for the wrong reason")
	}

	for _, c := range []struct {
		what  string
		image string
		env   []string
	}{
		{"an exec in the service container", "", nil},
		{"an ephemeral client image", "docker.io/rclone/rclone:latest", []string{"SECRET=" + password}},
	} {
		args, _ := entityCommandArgs("mysql", c.image, c.env, "some-cmd", false)
		for i, a := range args {
			if strings.Contains(a, password) {
				t.Errorf("%s puts the install's database password in argv[%d]; every site on this machine can read it out of /proc", c.what, i)
			}
		}
	}
}

// An action can override its entity's client image, so one entity lists
// through one tool and streams archives through another.
func TestActionRuntimeOverride(t *testing.T) {
	writeCustomService(t, "myengine", `name: myengine
image: example/engine:1
introspect:
  entities:
    - kind: buckets
      list: list-cmd
      image: example/client:1
      env:
        - CLIENT=1
      actions:
        create: create-cmd {{name}}
        export:
          exec: export-cmd {{name}}
          filename: "{{name}}.tar"
          image: example/streamer:1
          env:
            - STREAMER=1
`)
	spec := EntityFor("myengine", "buckets")
	image, env := actionRuntime(spec, spec.Actions["create"])
	if image != "example/client:1" || len(env) != 1 || env[0] != "CLIENT=1" {
		t.Errorf("create runtime = %q %v", image, env)
	}
	image, env = actionRuntime(spec, spec.Actions["export"])
	if image != "example/streamer:1" || len(env) != 1 || env[0] != "STREAMER=1" {
		t.Errorf("export runtime = %q %v", image, env)
	}
	if got := EntityExportFilename("myengine", "buckets", "photos"); got != "photos.tar" {
		t.Errorf("filename = %q", got)
	}
	if got := EntityExportFilename("myengine", "nokind", "photos"); got != "photos.dump" {
		t.Errorf("fallback filename = %q", got)
	}
	// The fallback becomes an output path in the CLI export paths, so a
	// name that never passed validation must not shape it.
	if got := EntityExportFilename("myengine", "nokind", "../../../etc/passwd"); strings.Contains(got, "/") {
		t.Errorf("fallback filename carries a path: %q", got)
	}
}

// A snapshot stores the same dump an export produces, gzipped in the container
// so the bytes cross podman exec compressed.
func TestEntitySnapshotCommandsWrapDeclaredActions(t *testing.T) {
	dump := entitySnapshotDumpCommand("export-cmd shop")
	if !strings.Contains(dump, "export-cmd shop") || !strings.Contains(dump, "gzip -c") {
		t.Errorf("dump = %q", dump)
	}
	restore := entitySnapshotRestoreCommand("import-cmd shop")
	if !strings.HasPrefix(restore, "gunzip -c") || !strings.Contains(restore, "import-cmd shop") {
		t.Errorf("restore = %q", restore)
	}
}

// A snapshot downloads in the engine's own dump format, so its filename must
// take the extension from the declared export, not a hardcoded .sql: a mongo
// snapshot is a mongodump archive, and saving it as db.sql mislabels it.
func TestSnapshotExportFilename(t *testing.T) {
	writeCustomService(t, "mongo", `name: mongo
image: docker.io/library/mongo:7
introspect:
  entities:
    - kind: databases
      list: echo x
      actions:
        export:
          exec: mongodump --archive
          filename: "{{name}}.archive"
`)
	if got := SnapshotExportFilename("mongo", "before-mutation-20260101-000000"); got != "before-mutation-20260101-000000.archive" {
		t.Errorf("mongo snapshot filename = %q, want .archive", got)
	}
	// A bundled SQL engine keeps .sql from its declared filename.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := SnapshotExportFilename("postgres", "nightly"); got != "nightly.sql" {
		t.Errorf("postgres snapshot filename = %q, want nightly.sql", got)
	}
	// An engine that declares no export filename falls back to a neutral, safe name.
	if got := SnapshotExportFilename("mysql", "../etc/passwd"); strings.Contains(got, "/") {
		t.Errorf("snapshot filename carries a path: %q", got)
	}
}

func TestDeclaresDatabasesFollowsTheDeclaration(t *testing.T) {
	writeCustomService(t, "myengine", entityEngineYAML)
	if DeclaresDatabases("myengine") {
		t.Error("an engine declaring only buckets must not count as a database engine")
	}
	writeCustomService(t, "analytics", `name: analytics
image: example/analytics:1
introspect:
  entities:
    - kind: databases
      list: list-dbs
`)
	if !DeclaresDatabases("analytics") {
		t.Error("a declared databases entity must count whatever the family is")
	}
	writeCustomService(t, "legacy", "name: legacy\nimage: example/legacy:1\nintrospect:\n  list_databases: echo hi\n")
	if !DeclaresDatabases("legacy") {
		t.Error("the legacy list_databases field must count too")
	}
}
