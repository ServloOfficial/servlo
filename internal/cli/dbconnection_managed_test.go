package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/dbconn"
)

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

func writeCAFile(t *testing.T) string {
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
	path := filepath.Join(t.TempDir(), "ca-certificate.crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A managed connection is reached before it is written down, and the failure is
// the one the operator has to act on rather than a generic one.
func TestDbConnectionAdd_TestsBeforeItSaves(t *testing.T) {
	isolateInstall(t)
	t.Setenv("SERVLO_DB_PASSWORD", "s3cret")
	stubConnectionTest(t, errors.New("cannot reach db.example.net:25060: connection refused"))

	err := runDbConnectionAdd("managed", "", "postgres", "db.example.net", 25060, "doadmin", "require", "")

	if err == nil {
		t.Fatal("a connection nothing answers on was saved")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %q, does not carry what went wrong", err)
	}
	if connectionExists(t, "managed") {
		t.Error("the connection was written down anyway")
	}
}

// It saves once the database answers.
func TestDbConnectionAdd_SavesOnceItAnswers(t *testing.T) {
	isolateInstall(t)
	t.Setenv("SERVLO_DB_PASSWORD", "s3cret")
	stubConnectionTest(t, nil)

	if err := runDbConnectionAdd("managed", "", "postgres", "db.example.net", 25060, "doadmin", "require", ""); err != nil {
		t.Fatal(err)
	}
	c, err := dbconn.Named("managed")
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "db.example.net" || c.Port != 25060 || c.TLSMode != dbconn.TLSRequire {
		t.Errorf("stored connection = %+v", c)
	}
}

// --ca-cert names the provider's download. Servlo takes a copy rather than
// remembering a path in somebody's home directory that a tidy-up will delete.
func TestDbConnectionAdd_TakesACopyOfTheCACertificate(t *testing.T) {
	isolateInstall(t)
	t.Setenv("SERVLO_DB_PASSWORD", "s3cret")
	stubConnectionTest(t, nil)
	src := writeCAFile(t)

	if err := runDbConnectionAdd("managed", "", "postgres", "db.example.net", 25060, "doadmin", "verify-ca", src); err != nil {
		t.Fatal(err)
	}

	c, err := dbconn.Named("managed")
	if err != nil {
		t.Fatal(err)
	}
	if c.CACert != dbconn.CACertPath("managed") {
		t.Errorf("ca_cert = %q, want servlo's own copy at %q", c.CACert, dbconn.CACertPath("managed"))
	}
	if _, err := os.Stat(c.CACert); err != nil {
		t.Errorf("the certificate was not copied: %v", err)
	}
}

// A file that is not a certificate is refused by name, before anything is
// saved. Storing it and failing at the first handshake is a connection that
// looks configured and is not.
func TestDbConnectionAdd_RefusesACACertificateThatIsNotOne(t *testing.T) {
	isolateInstall(t)
	t.Setenv("SERVLO_DB_PASSWORD", "s3cret")
	stubConnectionTest(t, nil)

	notACert := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(notACert, []byte("<html>sign in to download</html>"), 0644); err != nil {
		t.Fatal(err)
	}

	err := runDbConnectionAdd("managed", "", "postgres", "db.example.net", 25060, "doadmin", "verify-ca", notACert)

	if err == nil {
		t.Fatal("a file that is not a certificate was accepted")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("error = %q, does not say the file is not a certificate", err)
	}
	if connectionExists(t, "managed") {
		t.Error("the connection was saved with an unusable certificate")
	}
}

// Testing a connection that is already configured is its own command, because
// what usually changed is on the provider's side.
func TestDbConnectionTest_ReportsWhatTheDatabaseSaid(t *testing.T) {
	isolateInstall(t)

	reg := &dbconn.Registry{}
	if err := reg.Add(dbconn.External("managed", "mysql", "db.example.net", 3306, "admin", "pw")); err != nil {
		t.Fatal(err)
	}
	if err := dbconn.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	stubConnectionTest(t, nil)
	if err := runDbConnectionTest("managed"); err != nil {
		t.Fatalf("a working connection reported: %v", err)
	}

	stubConnectionTest(t, errors.New("db.example.net:3306 refused the credentials for user \"admin\""))
	err := runDbConnectionTest("managed")
	if err == nil {
		t.Fatal("a refused connection reported success")
	}
	if !strings.Contains(err.Error(), "refused the credentials") {
		t.Errorf("error = %q, does not carry what the database said", err)
	}
}

// Removing a connection takes its certificate and the accounts servlo created
// on it, so a name reused later starts clean.
func TestDbConnectionRemove_ForgetsTheCertificateAndTheAccounts(t *testing.T) {
	isolateInstall(t)
	t.Setenv("SERVLO_DB_PASSWORD", "s3cret")
	stubConnectionTest(t, nil)

	if err := runDbConnectionAdd("managed", "", "postgres", "db.example.net", 25060, "doadmin", "verify-ca", writeCAFile(t)); err != nil {
		t.Fatal(err)
	}
	if err := dbconn.RecordSiteUser("managed", "shop", "shop", "pw"); err != nil {
		t.Fatal(err)
	}

	if err := runDbConnectionRemove("managed"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dbconn.CACertPath("managed")); err == nil {
		t.Error("the certificate outlived the connection")
	}
	if _, ok := dbconn.SiteUserFor("managed", "shop"); ok {
		t.Error("an account on the removed connection is still recorded")
	}
}
