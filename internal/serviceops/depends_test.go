package serviceops

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/presetfixtures"
)

func init() { config.SetExtraPresetsForTest(presetfixtures.FS()) }

func withServiceHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
}

func writeDepQuadlet(t *testing.T, unit string) {
	t.Helper()
	dir := config.QuadletDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, unit+".container"), []byte("[Container]\nImage=x\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDependency_ExactMatch(t *testing.T) {
	withServiceHome(t)
	writeDepQuadlet(t, "servlo-mysql")

	if got := ResolveDependency("mysql"); got != "mysql" {
		t.Errorf("ResolveDependency(mysql) = %q, want mysql", got)
	}
}

func TestResolveDependency_FamilyMember(t *testing.T) {
	withServiceHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name: "postgres-pgvector-18", Image: "x", Family: "postgres", Preset: "postgres-pgvector",
	}); err != nil {
		t.Fatal(err)
	}

	if got := ResolveDependency("postgres"); got != "postgres-pgvector-18" {
		t.Errorf("ResolveDependency(postgres) = %q, want postgres-pgvector-18", got)
	}
}

func TestResolveDependency_EnvRoleDropIn(t *testing.T) {
	withServiceHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mariadb-11-8", Image: "x", Family: "mariadb", EnvRole: "mysql", Preset: "mariadb",
	}); err != nil {
		t.Fatal(err)
	}

	if got := ResolveDependency("mysql"); got != "mariadb-11-8" {
		t.Errorf("ResolveDependency(mysql) = %q, want mariadb-11-8", got)
	}
}

func TestResolveDependency_ValkeySatisfiesRedis(t *testing.T) {
	withServiceHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name: "valkey", Image: "x", Family: "valkey", EnvRole: "redis", Preset: "valkey",
	}); err != nil {
		t.Fatal(err)
	}

	if got := ResolveDependency("redis"); got != "valkey" {
		t.Errorf("ResolveDependency(redis) = %q, want valkey", got)
	}
}

func TestResolveDependency_PrefersLiteral(t *testing.T) {
	withServiceHome(t)
	writeDepQuadlet(t, "servlo-mysql")
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mariadb-11-8", Image: "x", Family: "mariadb", EnvRole: "mysql",
	}); err != nil {
		t.Fatal(err)
	}

	if got := ResolveDependency("mysql"); got != "mysql" {
		t.Errorf("ResolveDependency(mysql) = %q, want mysql when both installed", got)
	}
}

func TestResolveDependency_PrefersFamilyOverEnvRole(t *testing.T) {
	withServiceHome(t)
	// No bare mysql: versioned mysql and mariadb both satisfy. Same-family
	// must win over the env_role drop-in (alphabetical would pick mariadb).
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mariadb-12-3", Image: "x", Family: "mariadb", EnvRole: "mysql", Preset: "mariadb",
	}); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mysql-9-7", Image: "x", Family: "mysql", Preset: "mysql",
	}); err != nil {
		t.Fatal(err)
	}

	if got := ResolveDependency("mysql"); got != "mysql-9-7" {
		t.Errorf("ResolveDependency(mysql) = %q, want mysql-9-7 (family over env_role)", got)
	}
	if got := DependencyDisplayName("mysql"); got != "mysql" {
		t.Errorf("DependencyDisplayName(mysql) = %q, want mysql", got)
	}
}

func TestResolveDependency_NothingInstalled(t *testing.T) {
	withServiceHome(t)
	if got := ResolveDependency("mysql"); got != "" {
		t.Errorf("ResolveDependency(mysql) = %q, want empty", got)
	}
}

func TestMissingPresetDependencies_EnvRoleDropInOK(t *testing.T) {
	withServiceHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mariadb-11-8", Image: "x", Family: "mariadb", EnvRole: "mysql",
	}); err != nil {
		t.Fatal(err)
	}

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "phpmyadmin", DependsOn: []string{"mysql"},
		DynamicEnv: map[string]string{"PMA_HOSTS": "discover_family:mysql,mariadb"},
	})
	if len(missing) != 0 {
		t.Errorf("mariadb should satisfy mysql dep, got missing=%v", missing)
	}
}

func TestMissingPresetDependencies_ValkeyOKForRedisInsight(t *testing.T) {
	withServiceHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name: "valkey", Image: "x", Family: "valkey", EnvRole: "redis",
	}); err != nil {
		t.Fatal(err)
	}

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "redisinsight", DependsOn: []string{"redis"},
		Environment: map[string]string{"RI_REDIS_HOST": "servlo-redis"},
	})
	if len(missing) != 0 {
		t.Errorf("valkey should satisfy redisinsight's redis dep when it pins servlo-redis for the host rewrite, got missing=%v", missing)
	}
}

func TestMissingPresetDependencies_MentionsAlternatives(t *testing.T) {
	withServiceHome(t)

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "phpmyadmin", DependsOn: []string{"mysql"},
		AdminFor: []string{"mysql", "mariadb"},
	})
	if len(missing) != 1 {
		t.Fatalf("expected one missing dep, got %v", missing)
	}
	if !strings.Contains(missing[0], "mysql") || !strings.Contains(missing[0], "mariadb") {
		t.Errorf("missing label should mention mysql and mariadb, got %q", missing[0])
	}
}

func TestMissingPresetDependencies_MentionsStoreDropIns(t *testing.T) {
	withServiceHome(t)
	prev := ListStoreDropIns
	ListStoreDropIns = func(dep string) []string {
		if dep == "mysql" {
			return []string{"mariadb"}
		}
		return nil
	}
	t.Cleanup(func() { ListStoreDropIns = prev })

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "phpmyadmin", DependsOn: []string{"mysql"},
	})
	if len(missing) != 1 || !strings.Contains(missing[0], "mariadb") {
		t.Errorf("store drop-ins should appear in the missing label, got %v", missing)
	}
}

func TestMissingPresetDependencies_FamilyMemberWithPinnedHost(t *testing.T) {
	withServiceHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mongo-7", Image: "x", Family: "mongo",
	}); err != nil {
		t.Fatal(err)
	}

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "mongo-express", DependsOn: []string{"mongo"},
		Environment: map[string]string{
			"ME_CONFIG_MONGODB_URL": "mongodb://root:servlo@servlo-mongo:27017/",
		},
	})
	if len(missing) != 0 {
		t.Errorf("mongo-7 should satisfy mongo dep via family when the URL pins servlo-mongo for the rewrite, got missing=%v", missing)
	}
}

func TestMissingPresetDependencies_UnbindableDropInStaysStrict(t *testing.T) {
	withServiceHome(t)
	// Valkey meets redis via env_role, but a consumer that neither pins
	// servlo-redis (nothing for the host rewrite to retarget) nor declares
	// discover_family cannot bind the drop-in and must stay refused, otherwise
	// install succeeds and the UI times out talking to a missing hostname.
	if err := config.SaveCustomService(&config.CustomService{
		Name: "valkey", Image: "x", Family: "valkey", EnvRole: "redis",
	}); err != nil {
		t.Fatal(err)
	}

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "redisinsight", DependsOn: []string{"redis"},
		Environment: map[string]string{"RI_REDIS_HOST": "some-external-host"},
	})
	if len(missing) != 1 || !strings.Contains(missing[0], "redis") {
		t.Errorf("unbindable drop-in consumer must stay strict, got missing=%v", missing)
	}
}

func TestDependencyDisplayName_UsesPresetNotVersionedName(t *testing.T) {
	withServiceHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mariadb-11-8", Image: "x", Family: "mariadb", EnvRole: "mysql", Preset: "mariadb",
	}); err != nil {
		t.Fatal(err)
	}

	if got := DependencyDisplayName("mysql"); got != "mariadb" {
		t.Errorf("DependencyDisplayName(mysql) = %q, want mariadb", got)
	}
}

func TestDependencyDisplayName_PrefersRunningSatisfier(t *testing.T) {
	withServiceHome(t)
	writeDepQuadlet(t, "servlo-mysql")
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mariadb-11-8", Image: "x", Family: "mariadb", EnvRole: "mysql", Preset: "mariadb",
	}); err != nil {
		t.Fatal(err)
	}
	prev := config.ServiceRunning
	config.ServiceRunning = func(name string) bool { return name == "mariadb-11-8" }
	t.Cleanup(func() { config.ServiceRunning = prev })

	if got := DependencyDisplayName("mysql"); got != "mariadb" {
		t.Errorf("DependencyDisplayName(mysql) = %q, want mariadb (running drop-in over stopped literal)", got)
	}
}

func TestDependencyDisplayName_UnsatisfiedKeepsDeclared(t *testing.T) {
	withServiceHome(t)
	if got := DependencyDisplayName("mysql"); got != "mysql" {
		t.Errorf("DependencyDisplayName(mysql) = %q, want mysql", got)
	}
}

func TestDependencyDisplayName_ExactBuiltin(t *testing.T) {
	withServiceHome(t)
	writeDepQuadlet(t, "servlo-mysql")
	if got := DependencyDisplayName("mysql"); got != "mysql" {
		t.Errorf("DependencyDisplayName(mysql) = %q, want mysql", got)
	}
}

func TestDependentNeedsCascade_AlternateRemains(t *testing.T) {
	withServiceHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mariadb-12-3", Image: "x", Family: "mariadb", EnvRole: "mysql", Preset: "mariadb",
	}); err != nil {
		t.Fatal(err)
	}
	writeDepQuadlet(t, "servlo-mysql")
	if err := config.SaveCustomService(&config.CustomService{
		Name: "phpmyadmin", Image: "x", DependsOn: []string{"mysql"},
	}); err != nil {
		t.Fatal(err)
	}
	// Both installed and "running" (ServiceRunning nil → installed counts).
	if dependentNeedsCascade("phpmyadmin", "mysql") {
		t.Fatal("cascade must not run when mariadb still satisfies mysql")
	}
	if got := dependentsOf("mysql"); len(got) != 1 || got[0] != "phpmyadmin" {
		t.Errorf("dependentsOf(mysql) = %v, want [phpmyadmin]", got)
	}
}

func TestDependentNeedsCascade_LastSatisfier(t *testing.T) {
	withServiceHome(t)
	writeDepQuadlet(t, "servlo-mysql")
	if err := config.SaveCustomService(&config.CustomService{
		Name: "phpmyadmin", Image: "x", DependsOn: []string{"mysql"},
	}); err != nil {
		t.Fatal(err)
	}

	if !dependentNeedsCascade("phpmyadmin", "mysql") {
		t.Fatal("cascade must run when mysql is the only satisfier")
	}
}

func TestDependentNeedsCascade_StoppedAlternateDoesNotCount(t *testing.T) {
	withServiceHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mariadb-12-3", Image: "x", Family: "mariadb", EnvRole: "mysql", Preset: "mariadb",
	}); err != nil {
		t.Fatal(err)
	}
	writeDepQuadlet(t, "servlo-mysql")
	if err := config.SaveCustomService(&config.CustomService{
		Name: "phpmyadmin", Image: "x", DependsOn: []string{"mysql"},
	}); err != nil {
		t.Fatal(err)
	}
	prev := config.ServiceRunning
	config.ServiceRunning = func(name string) bool { return name == "mysql" }
	t.Cleanup(func() { config.ServiceRunning = prev })

	// mariadb is installed but stopped; stopping mysql is the last running
	// satisfier so phpMyAdmin must cascade-stop.
	if !dependentNeedsCascade("phpmyadmin", "mysql") {
		t.Fatal("cascade must run when the only other satisfier is stopped")
	}
}

func TestDependentsOf_EnvRoleDropIn(t *testing.T) {
	withServiceHome(t)
	if err := config.SaveCustomService(&config.CustomService{
		Name: "mariadb-12-3", Image: "x", Family: "mariadb", EnvRole: "mysql",
	}); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCustomService(&config.CustomService{
		Name: "phpmyadmin", Image: "x", DependsOn: []string{"mysql"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := dependentsOf("mariadb-12-3"); len(got) != 1 || got[0] != "phpmyadmin" {
		t.Errorf("dependentsOf(mariadb) = %v, want [phpmyadmin]", got)
	}
}

// An admin UI that reads its server list out of the connection registry is
// satisfied by a managed database, which has no container to start. Without
// this, an install whose only MySQL is on DigitalOcean cannot install
// phpMyAdmin at all: the dep resolves to nothing local and the install refuses,
// even though the tool would come up pointed straight at the managed server.
func TestMissingPresetDependencies_AManagedConnectionSatisfiesTheDep(t *testing.T) {
	withServiceHome(t)

	prev := config.DatabaseConnections
	config.DatabaseConnections = func(families []string) []config.DBConnectionInfo {
		if slices.Contains(families, "mysql") {
			return []config.DBConnectionInfo{{Name: "managed", Family: "mysql", Host: "db.example.net", Port: 25060}}
		}
		return nil
	}
	t.Cleanup(func() { config.DatabaseConnections = prev })

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "phpmyadmin", DependsOn: []string{"mysql"},
		DynamicEnv: map[string]string{"PMA_HOSTS": "connections:mysql,mariadb=hostport"},
	})
	if len(missing) != 0 {
		t.Errorf("a managed mysql connection should satisfy the dep, got missing=%v", missing)
	}
}

// The escape hatch is not a blanket one. A tool that does not read the registry
// still needs something running: pointing RedisInsight at a connection list it
// never consults would install it green and leave it talking to nothing.
func TestMissingPresetDependencies_AConnectionDoesNotSatisfyAToolThatIgnoresIt(t *testing.T) {
	withServiceHome(t)

	prev := config.DatabaseConnections
	config.DatabaseConnections = func([]string) []config.DBConnectionInfo {
		return []config.DBConnectionInfo{{Name: "managed", Family: "mysql", Host: "db.example.net", Port: 25060}}
	}
	t.Cleanup(func() { config.DatabaseConnections = prev })

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "somequeueui", DependsOn: []string{"mysql"},
		Environment: map[string]string{"DB_HOST": "servlo-mysql"},
	})
	if len(missing) != 1 {
		t.Errorf("a tool that never reads the registry should still report the dep missing, got %v", missing)
	}
}

// The directive is not the whole answer: it says where the tool would look, not
// that anything is there. An install with no mysql of any kind still has a
// missing dependency, and saying otherwise would install phpMyAdmin against an
// empty server list.
func TestMissingPresetDependencies_TheDirectiveAloneDoesNotSatisfyTheDep(t *testing.T) {
	withServiceHome(t)

	prev := config.DatabaseConnections
	config.DatabaseConnections = func([]string) []config.DBConnectionInfo { return nil }
	t.Cleanup(func() { config.DatabaseConnections = prev })

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "phpmyadmin", DependsOn: []string{"mysql"},
		DynamicEnv: map[string]string{"PMA_HOSTS": "connections:mysql,mariadb=hostport"},
	})
	if len(missing) != 1 {
		t.Errorf("no connection and no container is still a missing dep, got %v", missing)
	}
}

// A directive for some other family is not this dep's answer. A tool listing
// postgres connections does not make a missing mysql acceptable.
func TestMissingPresetDependencies_ADirectiveForAnotherFamilyDoesNotCount(t *testing.T) {
	withServiceHome(t)

	prev := config.DatabaseConnections
	config.DatabaseConnections = func([]string) []config.DBConnectionInfo {
		return []config.DBConnectionInfo{{Name: "managed", Family: "postgres", Host: "db.example.net", Port: 25060}}
	}
	t.Cleanup(func() { config.DatabaseConnections = prev })

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "pgadmin", DependsOn: []string{"mysql"},
		DynamicEnv: map[string]string{"SERVERS": "connections:postgres=hostport"},
	})
	if len(missing) != 1 {
		t.Errorf("a postgres directive should not satisfy a mysql dep, got %v", missing)
	}
}

// A local connection is a container by another name. Whether that container is
// installed is the question ResolveDependency already answers, and letting a
// local connection through here reported pgAdmin's postgres dep satisfied on an
// install with no postgres at all, because the registry names the local preset
// whether or not anything is running.
func TestMissingPresetDependencies_ALocalConnectionIsNotAnExcuseForAMissingContainer(t *testing.T) {
	withServiceHome(t)

	prev := config.DatabaseConnections
	config.DatabaseConnections = func([]string) []config.DBConnectionInfo {
		return []config.DBConnectionInfo{{Name: "postgres", Family: "postgres", Local: true, Host: "servlo-postgres", Port: 5432}}
	}
	t.Cleanup(func() { config.DatabaseConnections = prev })

	missing := MissingPresetDependencies(&config.CustomService{
		Name: "pgadmin", DependsOn: []string{"postgres"},
		DynamicEnv: map[string]string{"SERVERS": "connections:postgres=hostport"},
	})
	if len(missing) != 1 {
		t.Errorf("a local connection with no container is still a missing dep, got %v", missing)
	}
}
