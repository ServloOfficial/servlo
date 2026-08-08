package dbconn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func isolate(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	password, err := config.ServicePassword()
	if err != nil {
		t.Fatalf("ServicePassword: %v", err)
	}
	return password
}

// The password is the one the containers were started with. Anything else is a
// connection that fails to authenticate, which is what every caller reaching a
// database was doing before this package existed.
func TestForService_CarriesTheGeneratedPassword(t *testing.T) {
	password := isolate(t)

	c, err := ForService("mysql")
	if err != nil {
		t.Fatal(err)
	}
	if c.Password != password {
		t.Errorf("password = %q, want this install's generated one", c.Password)
	}
	if c.Password == "servlo" {
		t.Error("the connection carries the literal password the presets stopped using")
	}
}

// Rotating the password takes effect. A value read once at startup would keep a
// long-running panel authenticating with the old one until it restarted, which
// is the failure this is read-every-time to avoid.
func TestForService_ReadsThePasswordEachTime(t *testing.T) {
	isolate(t)

	rotated := strings.Repeat("r", 32)
	if err := os.WriteFile(config.ServicePasswordFile(), []byte(rotated), 0600); err != nil {
		t.Fatal(err)
	}

	c, err := ForService("mysql")
	if err != nil {
		t.Fatal(err)
	}
	if c.Password != rotated {
		t.Errorf("password = %q, want the rotated one", c.Password)
	}
}

// Each engine's client reads a different variable, and a command handed the
// wrong one prompts for a password that nobody is there to type.
func TestClientEnv_PerEngine(t *testing.T) {
	password := isolate(t)

	cases := map[string]string{
		"mysql":    "MYSQL_PWD=" + password,
		"postgres": "PGPASSWORD=" + password,
	}
	for family, want := range cases {
		c, err := ForFamily(family)
		if err != nil {
			t.Fatal(err)
		}
		got := c.ClientEnv()
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s: ClientEnv = %v, want [%s]", family, got, want)
		}
	}

	// An unknown family gets both, because a caller running a command in a
	// container it only knows by name should not have to guess which client
	// will read it. The engine ignores the one that is not its own.
	c, err := ForFamily("")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ClientEnv()) != 2 {
		t.Errorf("ClientEnv = %v, want both variables when the engine is unknown", c.ClientEnv())
	}
}

// The credential goes in the environment, never in the arguments: a password on
// a command line is readable out of the process list by every other user on the
// machine, and every site on this server runs as one of them.
func TestClientEnv_KeepsThePasswordOutOfArguments(t *testing.T) {
	password := isolate(t)

	c, err := ForService("mysql")
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range c.ClientEnv() {
		if !strings.Contains(pair, password) {
			t.Errorf("%q does not carry the password", pair)
		}
	}
}

// A local connection points at the service's container on the container
// network, on the port its engine answers.
func TestForService_AddressesTheContainer(t *testing.T) {
	isolate(t)

	c, err := ForService("postgres")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Local() {
		t.Error("a service servlo runs did not read as local")
	}
	if c.Host != "servlo-postgres" || c.Port != 5432 || c.User != "postgres" {
		t.Errorf("connection = %s@%s:%d, want postgres@servlo-postgres:5432", c.User, c.Host, c.Port)
	}
}

// The literals this package replaced, named so they cannot come back.
//
// Two of them already shipped: the framework definitions wrote a placeholder
// nobody substituted, and the Go that reaches into a database wrote a password
// no server had. Both were invisible until something tried to connect, which on
// a panel is a button that fails for a reason the operator cannot see, so the
// guard is a test rather than care.
func TestNoSourceWritesADatabasePasswordDown(t *testing.T) {
	banned := []string{"MYSQL_PWD=servlo", "PGPASSWORD=servlo", "-pservlo", "PASSWORD=servlo", ":servlo@"}

	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "dist", "build", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, literal := range banned {
			if strings.Contains(string(data), literal) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s writes %q, which is not the password any database here runs with", rel, literal)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
