package serviceops

import (
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/podman"
)

func TestWithURLPort(t *testing.T) {
	cases := []struct {
		in   string
		port int
		want string
	}{
		{"mysql://root:servlo@127.0.0.1:3306/servlo", 3307, "mysql://root:servlo@127.0.0.1:3307/servlo"},
		{"", 3307, ""},
		{"mysql://root:servlo@127.0.0.1:3306/servlo", 0, "mysql://root:servlo@127.0.0.1:3306/servlo"},
	}
	for _, c := range cases {
		if got := WithURLPort(c.in, c.port); got != c.want {
			t.Errorf("WithURLPort(%q, %d) = %q, want %q", c.in, c.port, got, c.want)
		}
	}
}

// TestWithDashboardPort covers a dashboard following a move of whichever mapping
// it rides on. A primary-port dashboard (meilisearch on 7700) follows the primary
// override; a secondary-port dashboard (rustfs' 9001 console)
// follows only a move of that secondary and stays put when the primary moves.
func TestWithDashboardPort(t *testing.T) {
	rustfsPorts := []string{"9000:9000", "9001:9001"}
	cases := []struct {
		name  string
		in    string
		ports []string
		cfg   config.ServiceConfig
		want  string
	}{
		{"primary override tracked", "http://localhost:7700", []string{"7700:7700"}, config.ServiceConfig{PublishedPort: 7701}, "http://localhost:7701"},
		{"secondary stays on primary move", "http://localhost:9001", rustfsPorts, config.ServiceConfig{PublishedPort: 9010}, "http://localhost:9001"},
		{"secondary override tracked", "http://localhost:9001", rustfsPorts, config.ServiceConfig{PublishedPorts: map[int]int{9001: 9002}}, "http://localhost:9002"},
		{"secondary override keeps path", "http://localhost:9001/rustfs/console/", rustfsPorts, config.ServiceConfig{PublishedPorts: map[int]int{9001: 9002}}, "http://localhost:9002/rustfs/console/"},
		{"no override is no-op", "http://localhost:9001", rustfsPorts, config.ServiceConfig{}, "http://localhost:9001"},
		{"proxy path has no host", "/_svc/redisinsight/", []string{"8085:5540"}, config.ServiceConfig{PublishedPort: 8090}, "/_svc/redisinsight/"},
		{"port not among mappings", "http://localhost:9999", rustfsPorts, config.ServiceConfig{PublishedPort: 9010}, "http://localhost:9999"},
		{"empty", "", rustfsPorts, config.ServiceConfig{}, ""},
	}
	for _, c := range cases {
		if got := WithDashboardPort(c.in, c.ports, c.cfg); got != c.want {
			t.Errorf("%s: WithDashboardPort(%q, %v) = %q, want %q", c.name, c.in, c.ports, got, c.want)
		}
	}
}

// TestMysqlPresetPortOverride validates the override against the real mysql
// preset: the canonical version publishes 3306, and moving the primary host
// port + connection URL to 3307 keeps the container-internal port at 3306.
func TestMysqlPresetPortOverride(t *testing.T) {
	p, err := config.LoadPreset("mysql")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := p.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if len(svc.Ports) == 0 || svc.Ports[0] != "3306:3306" {
		t.Fatalf("preset primary port = %v, want first entry 3306:3306", svc.Ports)
	}

	moved := podman.SetPrimaryHostPort(svc.Ports, 3307)
	if moved[0] != "3307:3306" {
		t.Errorf("moved primary port = %q, want 3307:3306 (container-internal port unchanged)", moved[0])
	}
	if url := WithURLPort(svc.ConnectionURL, 3307); !strings.Contains(url, ":3307/") {
		t.Errorf("connection URL after move = %q, want host port 3307", url)
	}
}
