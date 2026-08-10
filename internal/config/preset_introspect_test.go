package config

import (
	"strings"
	"testing"
)

// Every fresh postgres database inherits about 7.5 MB of system catalogs from
// template1, so a raw pg_database_size makes an empty database look like data.
// The mysql query already reports table data only; postgres has to net the
// template baseline off to say the same thing.
func TestPostgresListDatabases_NetsOffTheTemplateBaseline(t *testing.T) {
	p, err := LoadPreset("postgres")
	if err != nil {
		t.Fatalf("loading the postgres preset: %v", err)
	}
	spec := p.Introspect.DatabasesEntity()
	if spec == nil {
		t.Fatal("the postgres preset declares no databases entity")
	}
	q := spec.List
	if !strings.Contains(q, "template1") {
		t.Errorf("query reports the catalog baseline as data: %s", q)
	}
	if !strings.Contains(q, "GREATEST") {
		t.Errorf("query can report a negative size for a database below the baseline: %s", q)
	}
}

// A command that runs inside the engine's own container still has to say where
// the engine is.
//
// The client's compiled-in socket path and the server's configured one are two
// separate settings, and the pinned image has them disagreeing: mysqld listens
// on /var/lib/mysql/mysql.sock while the client looks for
// /var/run/mysqld/mysqld.sock. A command that leaves it to the default fails
// with "Can't connect to local MySQL server through socket" against an engine
// that is running, which reads like a dead database and is a client default.
// CI found it by creating a database on a real runner.
func TestMySQLLocalCommands_NameTheAddressRatherThanTrustTheDefaultSocket(t *testing.T) {
	p, err := LoadPreset("mysql")
	if err != nil {
		t.Fatalf("loading the mysql preset: %v", err)
	}
	spec := p.Introspect.DatabasesEntity()
	if spec == nil {
		t.Fatal("the mysql preset declares no databases entity")
	}

	local := map[string]string{"list": spec.List}
	for name, action := range spec.Actions {
		// The remote actions are aimed at a connection and take their host from
		// it; these are the ones that run beside the server.
		if strings.HasPrefix(name, "remote_") {
			continue
		}
		local[name] = action.Exec
	}
	if len(local) < 2 {
		t.Fatal("no local commands were checked, so this proves nothing")
	}
	for name, cmd := range local {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		if !strings.Contains(cmd, "-h 127.0.0.1") && !strings.Contains(cmd, "--socket") {
			t.Errorf("the %s command leaves the address to the client's default socket, which this image does not put where the server does: %s", name, cmd)
		}
	}
}

// An image on a databases entity is not a default, it is a routing decision.
//
// A command runs inside the service's own container unless the entity or the
// action names a client image, and then it runs in an ephemeral container of
// that image instead. A database client given no address falls back to a unix
// socket, and that socket only exists beside the server, so a local command
// routed into a client container is a command looking for an engine in a
// container that holds none. The symptom is `servlo db:create` failing against
// a database that is running, and CI found it on a real runner.
//
// Scoped to databases on purpose. An entity image is right where the service's
// own image ships no tooling for what it holds and the client addresses the
// server over the network anyway, which is what rustfs's buckets do. It is
// wrong for a database, whose client falls back to a socket instead.
func TestPresetDatabases_NameNoClientImageOnTheEntity(t *testing.T) {
	metas, err := ListPresets()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, meta := range metas {
		p, err := LoadPreset(meta.Name)
		if err != nil || p == nil || p.Introspect == nil {
			continue
		}
		for _, spec := range p.Introspect.Entities {
			if spec.Kind != "databases" {
				continue
			}
			checked++
			if spec.Image != "" {
				t.Errorf("%s: the databases entity names the client image %s, which sends its local commands into a container with no engine in it; put the image on the aimed actions that need it",
					meta.Name, spec.Image)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no preset declares a databases entity, so this proves nothing")
	}
}
