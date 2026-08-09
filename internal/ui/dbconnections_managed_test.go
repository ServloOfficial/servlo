package ui

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/dbconn"
)

// stubConnectionTest replaces the network reach with an answer, so the panel's
// own behaviour can be driven through both outcomes without a database.
func stubConnectionTest(t *testing.T, err error) *int {
	t.Helper()
	calls := 0
	prev := testConnection
	t.Cleanup(func() { testConnection = prev })
	testConnection = func(dbconn.Connection) error {
		calls++
		return err
	}
	return &calls
}

func stubServerIPs(t *testing.T, addrs []string, err error) {
	t.Helper()
	prev := serverIPs
	t.Cleanup(func() { serverIPs = prev })
	serverIPs = func() ([]string, error) { return addrs, err }
}

// connectionExists reports whether the registry holds the name. Named cannot
// answer this: a name it does not know resolves to the local service of that
// name, which is what keeps pre-connection installs working.
func connectionExists(t *testing.T, name string) bool {
	t.Helper()
	reg, err := dbconn.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	_, ok := reg.Find(name)
	return ok
}

func caPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "managed-db-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func connectionsGet(t *testing.T) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/db-connections", nil)
	rec := httptest.NewRecorder()
	handleDBConnections(rec, req)

	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// A connection that cannot be reached is not written down. Saving it and
// finding out at the first deploy means a site whose env file points at a
// database nothing here can open.
func TestDBConnections_TestsAManagedDatabaseBeforeSavingIt(t *testing.T) {
	setupConfigDirRaw(t, "", "", false)
	stubConnectionTest(t, errors.New("cannot reach db.example.net:25060: connection refused. If this is a managed database, add this server's public IP to the provider's trusted sources"))

	got := connectionsPost(t, `{"action":"add","name":"managed","engine":"postgres",
		"host":"db.example.net","port":25060,"user":"doadmin","password":"s3cret","tls_mode":"require"}`)

	message, _ := got["error"].(string)
	if message == "" {
		t.Fatal("a connection nothing answers on was saved")
	}
	if !strings.Contains(message, "trusted sources") {
		t.Errorf("error = %q, does not carry what went wrong", message)
	}
	if connectionExists(t, "managed") {
		t.Error("the connection was written down anyway")
	}
}

// A local connection is a container on a network the panel is not on, so there
// is nothing to dial before saving it.
func TestDBConnections_DoesNotDialALocalService(t *testing.T) {
	setupConfigDirRaw(t, "", "", false)
	calls := stubConnectionTest(t, errors.New("should not have been called"))

	got := connectionsPost(t, `{"action":"add","name":"local","service":"mysql"}`)
	// The service is not installed in a test config dir, so the add is refused
	// for that reason; what matters is that it was not refused for a failed dial.
	if message, _ := got["error"].(string); strings.Contains(message, "should not have been called") {
		t.Error("a local connection was tested over the network")
	}
	if *calls != 0 {
		t.Errorf("the network test ran %d times for a local connection", *calls)
	}
}

// The address to paste into a provider's trusted-sources list travels with the
// connection list, so the form can show it before the operator saves anything
// and the card can show it beside a connection that will not answer.
func TestDBConnections_CarriesThisServersPublicIP(t *testing.T) {
	setupConfigDirRaw(t, "", "", false)
	stubServerIPs(t, []string{"203.0.113.10"}, nil)

	got := connectionsGet(t)
	ips, _ := got["server_ips"].([]any)
	if len(ips) != 1 || ips[0] != "203.0.113.10" {
		t.Errorf("server_ips = %v, want this server's address", got["server_ips"])
	}
}

// A server that cannot work out its own address says so. An empty list reads as
// "nothing to add", which is the one conclusion that leaves the operator stuck.
func TestDBConnections_SaysWhenItCannotWorkOutThePublicIP(t *testing.T) {
	setupConfigDirRaw(t, "", "", false)
	stubServerIPs(t, nil, errors.New("no public address on any interface"))

	got := connectionsGet(t)
	if message, _ := got["server_ips_error"].(string); !strings.Contains(message, "public address") {
		t.Errorf("server_ips_error = %v, want the reason", got["server_ips_error"])
	}
}

// The provider hands over a .crt. It is uploaded, stored where servlo keeps its
// own configuration, and the connection carries the path rather than the file.
func TestDBConnections_StoresAnUploadedCACertificate(t *testing.T) {
	setupConfigDirRaw(t, "", "", false)
	stubConnectionTest(t, nil)
	cert := caPEM(t)

	body, err := json.Marshal(map[string]any{
		"action": "add", "name": "managed", "engine": "postgres",
		"host": "db.example.net", "port": 25060, "user": "doadmin", "password": "s3cret",
		"tls_mode": "verify-ca", "ca_cert_pem": cert,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := connectionsPost(t, string(body)); got["error"] != nil {
		t.Fatalf("add failed: %v", got["error"])
	}

	c, err := dbconn.Named("managed")
	if err != nil {
		t.Fatal(err)
	}
	if c.CACert != dbconn.CACertPath("managed") {
		t.Errorf("ca_cert = %q, want %q", c.CACert, dbconn.CACertPath("managed"))
	}
	data, err := os.ReadFile(c.CACert)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != cert {
		t.Error("the stored certificate is not the one that was uploaded")
	}
	if info, err := os.Stat(c.CACert); err == nil && info.Mode().Perm() != 0600 {
		t.Errorf("mode = %04o, want 0600", info.Mode().Perm())
	}
}

// An upload that is not a certificate is refused with a message that says so,
// and nothing is saved: a connection in verify-ca mode with an unusable file is
// a connection that fails at every handshake.
func TestDBConnections_RefusesACACertificateThatIsNotOne(t *testing.T) {
	setupConfigDirRaw(t, "", "", false)
	stubConnectionTest(t, nil)

	body, err := json.Marshal(map[string]any{
		"action": "add", "name": "managed", "engine": "postgres",
		"host": "db.example.net", "port": 25060, "user": "doadmin", "password": "s3cret",
		"tls_mode": "verify-ca", "ca_cert_pem": "<html>not a certificate</html>",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := connectionsPost(t, string(body))

	message, _ := got["error"].(string)
	if !strings.Contains(message, "certificate") {
		t.Errorf("error = %q, does not say the file is not a certificate", message)
	}
	if connectionExists(t, "managed") {
		t.Error("the connection was saved with an unusable certificate")
	}
}

// Testing an existing connection is its own action, because the reason to press
// it is that something changed on the provider's side rather than in the panel.
func TestDBConnections_TestsAConnectionThatIsAlreadySaved(t *testing.T) {
	setupConfigDirRaw(t, "", "", false)
	stubConnectionTest(t, nil)

	if got := connectionsPost(t, `{"action":"add","name":"managed","engine":"mysql",
		"host":"db.example.net","user":"admin","password":"pw"}`); got["error"] != nil {
		t.Fatalf("add failed: %v", got["error"])
	}

	if got := connectionsPost(t, `{"action":"test","name":"managed"}`); got["error"] != nil {
		t.Fatalf("test failed: %v", got["error"])
	}

	stubConnectionTest(t, errors.New("db.example.net:3306 refused the credentials for user \"admin\""))
	got := connectionsPost(t, `{"action":"test","name":"managed"}`)
	message, _ := got["error"].(string)
	if !strings.Contains(message, "refused the credentials") {
		t.Errorf("error = %q, does not carry what the database said", message)
	}
}

// Removing a connection takes its certificate with it, so a name reused later
// does not silently inherit the last connection's CA.
func TestDBConnections_RemovingAConnectionForgetsItsCertificate(t *testing.T) {
	setupConfigDirRaw(t, "", "", false)
	stubConnectionTest(t, nil)

	body, _ := json.Marshal(map[string]any{
		"action": "add", "name": "managed", "engine": "postgres",
		"host": "db.example.net", "port": 25060, "user": "doadmin", "password": "s3cret",
		"tls_mode": "verify-ca", "ca_cert_pem": caPEM(t),
	})
	if got := connectionsPost(t, string(body)); got["error"] != nil {
		t.Fatalf("add failed: %v", got["error"])
	}
	path := dbconn.CACertPath("managed")

	if got := connectionsPost(t, `{"action":"remove","name":"managed"}`); got["error"] != nil {
		t.Fatalf("remove failed: %v", got["error"])
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the certificate outlived the connection")
	}
}

// The certificate is a file, not a field. Nothing in the API response carries
// its contents, and nothing puts it in the registry beside the password.
func TestDBConnections_NeverRendersTheCertificateContents(t *testing.T) {
	setupConfigDirRaw(t, "", "", false)
	stubConnectionTest(t, nil)
	cert := caPEM(t)

	body, _ := json.Marshal(map[string]any{
		"action": "add", "name": "managed", "engine": "postgres",
		"host": "db.example.net", "port": 25060, "user": "doadmin", "password": "s3cret",
		"tls_mode": "verify-ca", "ca_cert_pem": cert,
	})
	if got := connectionsPost(t, string(body)); got["error"] != nil {
		t.Fatalf("add failed: %v", got["error"])
	}

	req := httptest.NewRequest(http.MethodGet, "/api/db-connections", nil)
	rec := httptest.NewRecorder()
	handleDBConnections(rec, req)
	if strings.Contains(rec.Body.String(), "BEGIN CERTIFICATE") {
		t.Errorf("the response carries the certificate itself:\n%s", rec.Body.String())
	}
}
