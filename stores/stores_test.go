package stores

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ServloOfficial/servlo/internal/config"
)

// The index is not the store. It is a summary of the store written by hand
// beside it, and every hand-written summary drifts: a definition added without
// its row is invisible to a client, a row without a definition advertises
// something no fetch can serve, and a digest left at the old value rejects the
// very file it is supposed to vouch for.
//
// These read the embedded tree rather than the working directory, because the
// embedded tree is what a binary actually ships.

type frameworkIndex struct {
	Frameworks []struct {
		Name     string            `json:"name"`
		Label    string            `json:"label"`
		Versions []string          `json:"versions"`
		Latest   string            `json:"latest"`
		Digests  map[string]string `json:"digests"`
	} `json:"frameworks"`
}

func TestFrameworkIndex_MatchesTheDefinitionsBesideIt(t *testing.T) {
	var index frameworkIndex
	readIndex(t, Frameworks, &index)

	onDisk := frameworkVersionsOnDisk(t)
	listed := map[string]bool{}

	for _, row := range index.Frameworks {
		listed[row.Name] = true
		versions, ok := onDisk[row.Name]
		if !ok {
			t.Errorf("the index lists %q, which has no definitions", row.Name)
			continue
		}
		for _, v := range row.Versions {
			if !versions[v] {
				t.Errorf("%s: the index lists version %q, which has no file", row.Name, v)
			}
		}
		for v := range versions {
			if !contains(row.Versions, v) {
				t.Errorf("%s: %s.yaml is in the store and not in the index", row.Name, v)
			}
		}
		if !contains(row.Versions, row.Latest) {
			t.Errorf("%s: latest is %q, which is not one of its versions", row.Name, row.Latest)
		}
		for v, digest := range row.Digests {
			data, ok := Read(Frameworks, row.Name+"/"+v+".yaml")
			if !ok {
				t.Errorf("%s: the index carries a digest for version %q, which has no file", row.Name, v)
				continue
			}
			sum := sha256.Sum256(data)
			if want := "sha256:" + hex.EncodeToString(sum[:]); digest != want {
				t.Errorf("%s %s: the index digest is %s and the file is %s, so a fetched copy would be rejected", row.Name, v, digest, want)
			}
		}
		for v := range versions {
			if row.Digests[v] == "" {
				t.Errorf("%s: version %q has no digest, so a fetched copy is taken on trust", row.Name, v)
			}
		}
	}

	for name := range onDisk {
		if !listed[name] {
			t.Errorf("%s is in the store and not in the index, so no client can discover it", name)
		}
	}
}

// A definition's own name and version have to agree with where it sits, or a
// client that fetched it by path gets back a definition for something else.
func TestFrameworkDefinitions_AgreeWithTheirPath(t *testing.T) {
	for name, versions := range frameworkVersionsOnDisk(t) {
		for v := range versions {
			data, _ := Read(Frameworks, name+"/"+v+".yaml")
			var def struct {
				Name    string `yaml:"name"`
				Version string `yaml:"version"`
				Label   string `yaml:"label"`
			}
			if err := yaml.Unmarshal(data, &def); err != nil {
				t.Errorf("%s/%s.yaml does not parse: %v", name, v, err)
				continue
			}
			if def.Name != name {
				t.Errorf("%s/%s.yaml calls itself %q", name, v, def.Name)
			}
			if def.Version != v {
				t.Errorf("%s/%s.yaml calls itself version %q", name, v, def.Version)
			}
			if strings.TrimSpace(def.Label) == "" {
				t.Errorf("%s/%s.yaml has no label, so the panel has nothing to show", name, v)
			}
		}
	}
}

// A framework definition is read at link time and its nginx block is spliced
// into a server block at vhost time, which is a long way from here. An
// unbalanced snippet closes the enclosing server and declares servers of its
// own, and the first thing that notices is `nginx -t` refusing to reload on
// somebody's server.
func TestFrameworkDefinitions_ParseAndTheirNginxBlocksAreSpliceable(t *testing.T) {
	for name, versions := range frameworkVersionsOnDisk(t) {
		for v := range versions {
			data, _ := Read(Frameworks, name+"/"+v+".yaml")
			var fw config.Framework
			if err := yaml.Unmarshal(data, &fw); err != nil {
				t.Errorf("%s/%s.yaml does not parse as a framework: %v", name, v, err)
				continue
			}
			if err := config.ValidatePublicDir(fw.PublicDir); err != nil {
				t.Errorf("%s/%s.yaml: %v", name, v, err)
			}
			if fw.Nginx == nil {
				continue
			}
			if err := config.ValidateNginxSnippet(fw.Nginx.Snippet); err != nil {
				t.Errorf("%s/%s.yaml nginx block: %v", name, v, err)
			}
		}
	}
}

func TestServiceIndex_MatchesThePresetsBesideIt(t *testing.T) {
	var index struct {
		Services []struct {
			Name string `json:"name"`
		} `json:"services"`
	}
	readIndex(t, Services, &index)

	onDisk := map[string]bool{}
	entries, err := fs.ReadDir(FS(), string(Services))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		onDisk[strings.TrimSuffix(e.Name(), ".yaml")] = true
	}

	for _, row := range index.Services {
		if !onDisk[row.Name] {
			t.Errorf("the index lists the preset %q, which has no file", row.Name)
		}
		delete(onDisk, row.Name)
	}
	for name := range onDisk {
		t.Errorf("%s.yaml is in the store and not in the index, so no client can discover it", name)
	}
}

func readIndex(t *testing.T, kind Kind, into any) {
	t.Helper()
	data, ok := Read(kind, "index.json")
	if !ok {
		t.Fatalf("the %s store has no index", kind)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("reading the %s index: %v", kind, err)
	}
}

// frameworkVersionsOnDisk returns name -> set of versions, read from the
// embedded tree rather than from the index it is checked against.
func frameworkVersionsOnDisk(t *testing.T) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	err := fs.WalkDir(FS(), string(Frameworks), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return err
		}
		name := path.Base(path.Dir(p))
		if out[name] == nil {
			out[name] = map[string]bool{}
		}
		out[name][strings.TrimSuffix(path.Base(p), ".yaml")] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("the framework store ships nothing, so this proves nothing")
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// A command that runs beside its own server still has to say where the server
// is, for the MySQL family.
//
// The client's compiled-in socket path and the server's configured one are two
// separate settings, and the MySQL image servlo pins has them disagreeing: the
// server listens on /var/lib/mysql/mysql.sock while the client looks for
// /var/run/mysqld/mysqld.sock. A command that leaves it to the default fails
// with "Can't connect to local MySQL server through socket" against an engine
// that is running, which reads like a dead database and is a client default.
//
// Scoped to the family that has been seen to disagree. Postgres is deliberately
// not held to this: its client and server agree on the socket, and moving those
// commands to TCP would change how they authenticate, which is a different and
// riskier thing than naming an address.
func TestServicePresets_MySQLFamilyCommandsNameTheAddress(t *testing.T) {
	entries, err := fs.ReadDir(FS(), string(Services))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, _ := Read(Services, e.Name())
		for i, line := range strings.Split(string(data), "\n") {
			// -uroot is the local spelling; the aimed commands take
			// -u {{admin_user}} and carry a host already.
			if !strings.Contains(line, "-uroot") {
				continue
			}
			checked++
			if !strings.Contains(line, "-h ") && !strings.Contains(line, "--socket") {
				t.Errorf("%s:%d leaves the address to the client's default socket: %s",
					e.Name(), i+1, strings.TrimSpace(line))
			}
		}
	}
	if checked == 0 {
		t.Fatal("no local MySQL-family command was checked, so this proves nothing")
	}
}
