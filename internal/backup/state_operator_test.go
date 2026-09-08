package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// writeUnder puts a file at rel inside the data directory.
func writeUnder(t *testing.T, rel, body string) string {
	t.Helper()
	p := filepath.Join(config.DataDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The files servlo promises never to overwrite are the files servlo did not
// back up.
//
// Three directories under the data directory carry an explicit never-clobber
// contract, stated in their own doc comments: nginx custom.d and http.d say
// "servlo never writes here, so edits survive vhost regeneration and `servlo
// update`", and a service tuning file says its edits survive `servlo service
// reinstall` and `servlo update`. The per-version and per-site PHP inis are the
// same contract in a third place.
//
// The server-state archive carried the config directory and, out of the data
// directory, sites.yaml alone, on the reasoning that the rest was caches,
// certificates that would be reissued, and the archives themselves. That is
// true of the rest and false of these: nothing else knows what the operator
// typed into them. So a rebuild came up with stock MySQL tuning, no custom
// nginx, and default PHP limits, having reported that the restore succeeded.
func TestCreateState_CarriesTheFilesTheOperatorWrote(t *testing.T) {
	configTree(t)
	key := testKey(t)

	writeUnder(t, "nginx/custom.d/acme-supply.com.conf", "location /health { return 200; }")
	writeUnder(t, "nginx/http.d/zz-servlo-user.conf", "client_max_body_size 512m;")
	writeUnder(t, "service-tuning/mysql.conf", "[mysqld]\nmax_allowed_packet = 1G")
	writeUnder(t, "php/8.4/98-user.ini", "memory_limit = 512M")
	writeUnder(t, "php/shared/95-shared.ini", "date.timezone = Europe/Berlin")
	writeUnder(t, "php/sites/acme/98-user.ini", "memory_limit = 1G")

	var sealed bytes.Buffer
	if _, err := CreateState(&sealed, key, StateOptions{}); err != nil {
		t.Fatal(err)
	}
	files := stateEntries(t, sealed.Bytes(), key)

	for name, want := range map[string]string{
		"data/nginx/custom.d/acme-supply.com.conf": "location /health { return 200; }",
		"data/nginx/http.d/zz-servlo-user.conf":    "client_max_body_size 512m;",
		"data/service-tuning/mysql.conf":           "[mysqld]\nmax_allowed_packet = 1G",
		"data/php/8.4/98-user.ini":                 "memory_limit = 512M",
		"data/php/shared/95-shared.ini":            "date.timezone = Europe/Berlin",
		"data/php/sites/acme/98-user.ini":          "memory_limit = 1G",
	} {
		got, ok := files[name]
		if !ok {
			t.Errorf("the state archive is missing %s, which nothing can reconstruct", name)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// What servlo writes itself stays out. A vhost is regenerated from the
// registry, and one restored from an archive names a certificate the rebuilt
// machine has not been issued yet. The backup directories are copies of the
// files beside them, and the aux tuning file is rewritten on every start.
func TestCreateState_LeavesOutWhatServloRegenerates(t *testing.T) {
	configTree(t)
	key := testKey(t)

	writeUnder(t, "nginx/conf.d/acme-supply.com.conf", "server { listen 443 ssl; }")
	writeUnder(t, "nginx/custom.d.bkp/acme-supply.com.conf.20260101", "an older copy")
	writeUnder(t, "nginx/htpasswd/staging.acme-supply.com", "staging:$2y$05$hash")
	writeUnder(t, "service-tuning/postgres.aux.conf", "include_dir = '/etc/postgres.d'")
	writeUnder(t, "service-tuning.bkp/mysql.conf.20260101", "an older copy")
	writeUnder(t, "php/8.4/ini.bkp/98-user.ini.20260101", "an older copy")
	writeUnder(t, "certs/sites/acme-supply.com/fullchain.pem", "-----BEGIN CERTIFICATE-----")
	writeUnder(t, "snapshots/whatever.json", "{}")

	var sealed bytes.Buffer
	if _, err := CreateState(&sealed, key, StateOptions{}); err != nil {
		t.Fatal(err)
	}
	files := stateEntries(t, sealed.Bytes(), key)

	for _, unwanted := range []string{
		"data/nginx/conf.d/acme-supply.com.conf",
		"data/nginx/custom.d.bkp/acme-supply.com.conf.20260101",
		"data/nginx/htpasswd/staging.acme-supply.com",
		"data/service-tuning/postgres.aux.conf",
		"data/service-tuning.bkp/mysql.conf.20260101",
		"data/php/8.4/ini.bkp/98-user.ini.20260101",
		"data/certs/sites/acme-supply.com/fullchain.pem",
		"data/snapshots/whatever.json",
	} {
		if _, ok := files[unwanted]; ok {
			t.Errorf("the state archive carries %s, which servlo writes for itself", unwanted)
		}
	}
}

// The whole claim, end to end: what the operator wrote comes back where the
// operator wrote it. The archive carrying a file and the restore putting it
// back are two things, and only the second one is the promise.
func TestStateRoundTrip_PutsTheOperatorsFilesBack(t *testing.T) {
	configTree(t)
	key := testKey(t)

	writeUnder(t, "nginx/custom.d/acme-supply.com.conf", "location /health { return 200; }")
	writeUnder(t, "service-tuning/mysql.conf", "[mysqld]\nmax_allowed_packet = 1G")
	writeUnder(t, "php/sites/acme/98-user.ini", "memory_limit = 1G")

	var sealed bytes.Buffer
	if _, err := CreateState(&sealed, key, StateOptions{}); err != nil {
		t.Fatal(err)
	}

	freshConfig, freshData := t.TempDir(), t.TempDir()
	if _, err := RestoreState(bytes.NewReader(sealed.Bytes()), key, freshConfig, freshData); err != nil {
		t.Fatal(err)
	}

	for rel, want := range map[string]string{
		"nginx/custom.d/acme-supply.com.conf": "location /health { return 200; }",
		"service-tuning/mysql.conf":           "[mysqld]\nmax_allowed_packet = 1G",
		"php/sites/acme/98-user.ini":          "memory_limit = 1G",
	} {
		body, err := os.ReadFile(filepath.Join(freshData, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s did not come back on the rebuilt machine: %v", rel, err)
			continue
		}
		if string(body) != want {
			t.Errorf("%s = %q, want %q", rel, body, want)
		}
	}
}
