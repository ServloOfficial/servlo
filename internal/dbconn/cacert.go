package dbconn

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
)

// A managed provider's CA certificate.
//
// DigitalOcean, RDS and the rest hand over a .crt and expect the client to
// verify the server against it. Servlo stores it rather than asking for a path,
// because a path is a file somebody else owns: it lands in a home directory, or
// worse inside the site tree, where nginx serves it and a deploy can delete it
// out from under a connection that was working yesterday.
//
// It goes beside the DNS provider credentials, in the config directory and
// owner-only. A CA certificate is not a secret, but the connection it belongs
// to is, and keeping the two together means one directory to secure and one
// place to look.

// caCertDir holds one certificate per connection.
func caCertDir() string { return filepath.Join(config.ConfigDir(), "db-ca") }

// CACertPath is where the named connection's certificate lives, whether or not
// one has been stored yet.
func CACertPath(name string) string { return filepath.Join(caCertDir(), name+".crt") }

// SaveCACert stores a connection's CA certificate and returns its path.
//
// The contents are checked before anything is written. A file that is not a
// certificate is discovered here, where the operator is looking at the upload
// they just chose, rather than at the first TLS handshake as a driver error
// about an empty certificate pool.
func SaveCACert(name string, data []byte) (string, error) {
	if !connectionName.MatchString(name) {
		return "", fmt.Errorf("%q is not a usable connection name: lowercase letters, digits and dashes, up to 40 characters", name)
	}
	if err := verifyCACert(data); err != nil {
		return "", err
	}
	if err := os.MkdirAll(caCertDir(), 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(caCertDir(), 0700); err != nil {
		return "", err
	}
	path := CACertPath(name)
	// Through a temp file so a half-written certificate never replaces a working
	// one, and 0600 from the start like everything else servlo keeps here.
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

// ImportCACert stores the certificate at path, for the CLI's --ca-cert.
func ImportCACert(name, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading the CA certificate: %w", err)
	}
	return SaveCACert(name, data)
}

// RemoveCACert forgets a connection's certificate. A connection that never had
// one is not an error to remove.
func RemoveCACert(name string) error {
	if !connectionName.MatchString(name) {
		return nil
	}
	if err := os.Remove(CACertPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// verifyCACert reports whether data is a PEM certificate, naming what it is
// instead when it is not. The two mistakes worth telling apart are the provider
// page's other download (a private key) and the page itself (HTML saved by a
// browser that followed a login redirect).
func verifyCACert(data []byte) error {
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if strings.Contains(block.Type, "PRIVATE KEY") {
			return fmt.Errorf("that file is a PEM private key, not a certificate: a managed provider's CA certificate is the file it offers as a .crt")
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return fmt.Errorf("that file has a PEM certificate block that does not parse as a certificate: %w", err)
		}
		return nil
	}
	return fmt.Errorf("that file is not a PEM certificate: it holds no -----BEGIN CERTIFICATE----- block. Download the CA certificate from the provider's console and upload that file")
}
