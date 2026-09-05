package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"github.com/ServloOfficial/servlo/internal/config"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configTree is a server's own state: the registry, the connections, the
// per-site accounts, a provider certificate, and the backup key that opens
// every archive this server has written.
func configTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg := filepath.Join(dir, "servlo")
	write := func(rel, body string) {
		p := filepath.Join(cfg, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("config.yaml", "production: true")
	write("databases.yaml", "connections: [{name: managed}]")
	write("database-users.yaml", "users: []")
	write("smtp.json", `{"host":"smtp.example.net"}`)
	write("db-ca/managed.crt", "-----BEGIN CERTIFICATE-----")
	write(keyFile, "aabbcc")
	return cfg
}

func stateEntries(t *testing.T, sealed []byte, key []byte) map[string]string {
	t.Helper()
	dec, err := NewDecryptor(bytes.NewReader(sealed), key)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name] = string(b)
	}
	return out
}

// A rebuild needs the registry and every setting beside it. Without them a
// restore brings back files on a server that does not know what a site is.
func TestCreateState_CarriesTheRegistryAndTheSettings(t *testing.T) {
	configTree(t)
	if err := os.WriteFile(filepath.Join(sitesDirFor(t), "sites.yaml"), []byte("sites: [{name: acme}]"), 0600); err != nil {
		t.Fatal(err)
	}
	key := testKey(t)

	var sealed bytes.Buffer
	if _, err := CreateState(&sealed, key, StateOptions{}); err != nil {
		t.Fatal(err)
	}
	files := stateEntries(t, sealed.Bytes(), key)

	for _, want := range []string{
		"config/config.yaml", "config/databases.yaml", "config/database-users.yaml",
		"config/smtp.json", "config/db-ca/managed.crt", "data/sites.yaml",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("the state archive is missing %s, which a rebuild needs", want)
		}
	}
}

// The one file that must never be in here. The key is what opens this archive,
// so shipping it inside would be locking the door and taping the key to it.
func TestCreateState_LeavesTheBackupKeyOut(t *testing.T) {
	configTree(t)
	key := testKey(t)

	var sealed bytes.Buffer
	if _, err := CreateState(&sealed, key, StateOptions{}); err != nil {
		t.Fatal(err)
	}
	files := stateEntries(t, sealed.Bytes(), key)

	for name, body := range files {
		if strings.Contains(name, keyFile) {
			t.Errorf("the backup key is in the archive it opens, at %s", name)
		}
		if strings.Contains(body, "aabbcc") {
			t.Errorf("the backup key's contents are in %s", name)
		}
	}
}

// It says what it is, so a restore can tell a server archive from a site's and
// refuse to unpack one over the other.
func TestCreateState_SaysItIsAServerArchive(t *testing.T) {
	configTree(t)
	key := testKey(t)

	var sealed bytes.Buffer
	man, err := CreateState(&sealed, key, StateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if man.Kind != KindState {
		t.Errorf("manifest kind = %q, want %q", man.Kind, KindState)
	}
	if man.Site != "" {
		t.Errorf("a server archive claims to be site %q", man.Site)
	}
}

// sitesDirFor is where the registry lives under the temporary data home.
func sitesDirFor(t *testing.T) string {
	t.Helper()
	dir := filepath.Dir(sitesFilePath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The two archive kinds unpack to completely different places, so each refuses
// the other. Getting them the wrong way round would empty a server's
// configuration over a site directory, or the reverse.
func TestRestore_EachKindRefusesTheOther(t *testing.T) {
	configTree(t)
	key := testKey(t)

	var serverArchive bytes.Buffer
	if _, err := CreateState(&serverArchive, key, StateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreFiles(bytes.NewReader(serverArchive.Bytes()), key, t.TempDir()); err == nil {
		t.Error("a server archive was unpacked over a site directory")
	} else if !strings.Contains(err.Error(), "--state") {
		t.Errorf("error = %q, does not say which command to use", err)
	}

	var siteArchive bytes.Buffer
	if _, err := Create(&siteArchive, key, Options{Site: siteConfigForState(t)}); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreState(bytes.NewReader(siteArchive.Bytes()), key, t.TempDir(), t.TempDir()); err == nil {
		t.Error("a site archive was unpacked over the server's configuration")
	}
}

func siteConfigForState(t *testing.T) config.Site {
	t.Helper()
	return config.Site{Name: "acme", Domains: []string{"acme-supply.com"}, Path: t.TempDir()}
}
