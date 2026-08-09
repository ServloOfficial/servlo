package dbconn

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
)

// Reaching a managed database over the network.
//
// Everything else in servlo talks to a database by running a client inside the
// container it is in. A managed database has no container, so this is the one
// place that opens a socket, and it is worth being careful about two things:
// what the operator is told when it fails, and what ends up in the message.
//
// The failure is nearly always one of four, and they take different actions:
// the packet never arrived (this server is not in the provider's trusted
// sources), the credentials were refused, the certificate did not verify, or
// the database answered and said no to something. A single "could not connect"
// makes the first indistinguishable from the second, which is how an operator
// spends an afternoon rotating a password that was never wrong.

// netTimeout bounds a dial and a query. A panel request waits on this, so it is
// short enough that a black-holed connection reports rather than hangs, and
// long enough for a managed database three regions away.
var netTimeout = 10 * time.Second

// identifier is what may be pasted into a statement no placeholder can carry.
// A database name and a user name are both identifiers, and neither engine lets
// them be bound, so they are checked instead of escaped.
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// Test opens the database with the stored credentials and reports what
// happened in words an operator can act on.
func Test(c Connection) error { return TestContext(context.Background(), c) }

// TestContext is Test under a caller's deadline.
func TestContext(ctx context.Context, c Connection) error {
	db, err := open(c)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, netTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return connectionError(c, err)
	}
	return nil
}

// CreateDatabaseAndUser creates the database and a user with rights to it and
// nothing else, and returns the password it generated for that user.
//
// It is safe to run twice. The database is created only if it is missing, and a
// user that is already there keeps the password it already has: rotating it
// would lock out every site whose env file carries the old one, which is a way
// to take a working server down on a re-run of a command that changed nothing.
// The password comes back empty in the one case servlo cannot answer for, a
// user that exists on the server but was not created here.
func CreateDatabaseAndUser(c Connection, database, username string) (string, error) {
	return createDatabaseAndUser(context.Background(), c, database, username)
}

func createDatabaseAndUser(ctx context.Context, c Connection, database, username string) (string, error) {
	if c.Local() {
		return "", fmt.Errorf("connection %q is the local service %s: its databases are created inside its container, not over the network",
			c.Name, c.Service)
	}
	if !identifier.MatchString(database) {
		return "", fmt.Errorf("%q is not a usable database name: letters, digits and underscores, starting with a letter", database)
	}
	if !identifier.MatchString(username) || len(username) > 32 {
		return "", fmt.Errorf("%q is not a usable database user name: letters, digits and underscores, up to 32 characters", username)
	}

	// A password servlo already generated for this database is the one the
	// site's env file carries, so it is reused rather than replaced.
	password := generatePassword()
	if recorded, ok := SiteUserFor(c.Name, database); ok && recorded.User == username {
		password = recorded.Password
	}

	db, err := open(c)
	if err != nil {
		return "", err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, netTimeout)
	defer cancel()

	existed, err := provision(ctx, db, c, database, username, password)
	if err != nil {
		return "", connectionError(c, err)
	}
	if existed {
		// The server has a user servlo did not create, so servlo does not hold
		// its password and must not invent one.
		if recorded, ok := SiteUserFor(c.Name, database); ok && recorded.User == username {
			return recorded.Password, nil
		}
		return "", nil
	}
	if c.Name != "" {
		if err := RecordSiteUser(c.Name, database, username, password); err != nil {
			return "", err
		}
	}
	return password, nil
}

// provision runs the engine's statements and reports whether the user was
// already there. Everything else is idempotent, so the only state worth
// carrying out is the one that decides whether the generated password is real.
func provision(ctx context.Context, db *sql.DB, c Connection, database, username, password string) (existed bool, err error) {
	if c.Family == "postgres" {
		// CREATE ROLE has no IF NOT EXISTS, so the duplicate is caught rather
		// than avoided.
		_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD %s`, pgIdent(username), pgLiteral(password)))
		switch {
		case isPgCode(err, "42710"):
			existed = true
		case err != nil:
			return false, err
		}
		for _, stmt := range postgresProvisionStatements(database, username) {
			if _, err := db.ExecContext(ctx, stmt); err != nil && !isPgCode(err, "42P04") {
				return existed, err
			}
		}
		return existed, nil
	}

	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE USER %s@'%%' IDENTIFIED BY %s", myIdent(username), myLiteral(password)))
	switch {
	case isMySQLCode(err, 1396):
		existed = true
	case err != nil:
		return false, err
	}
	for _, stmt := range mysqlProvisionStatements(database, username) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return existed, err
		}
	}
	return existed, nil
}

// mysqlProvisionStatements is everything after the user exists: the database,
// and a grant on that database alone. Server-wide privileges are what would let
// one site read every other site on the same server.
func mysqlProvisionStatements(database, username string) []string {
	return []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", myIdentBacktick(database)),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON %s.* TO %s@'%%'", myIdentBacktick(database), myIdent(username)),
	}
}

// postgresProvisionStatements is the same for PostgreSQL, plus the revoke.
//
// PostgreSQL grants CONNECT on a new database to PUBLIC, and every role in the
// cluster is in PUBLIC. On a managed server that cluster holds every other
// site, so leaving the default in place would give each site's user a way into
// all the others.
func postgresProvisionStatements(database, username string) []string {
	return []string{
		fmt.Sprintf(`CREATE DATABASE %s OWNER %s`, pgIdent(database), pgIdent(username)),
		fmt.Sprintf(`REVOKE CONNECT ON DATABASE %s FROM PUBLIC`, pgIdent(database)),
		fmt.Sprintf(`GRANT ALL PRIVILEGES ON DATABASE %s TO %s`, pgIdent(database), pgIdent(username)),
	}
}

// open builds the pool. It does not connect: database/sql dials on first use,
// which is the ping or the first statement, so a caller always gets the dial
// failure through connectionError rather than raw from here.
func open(c Connection) (*sql.DB, error) {
	if c.Local() {
		return nil, fmt.Errorf("connection %q is the local service %s: servlo reaches it through its container rather than over the network, so there is nothing to test here. `servlo service status %s` reports whether it is running",
			c.Name, c.Service, c.Service)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	driver, dsn, err := dataSource(c)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, connectionError(c, err)
	}
	// One connection, closed as soon as the caller is done: this pool exists for
	// a single test or a single provisioning run, not for serving traffic.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(netTimeout)
	return db, nil
}

// dataSource is the driver name and connection string for a connection. The
// password goes through the driver's own encoder rather than string
// concatenation, so a credential with a punctuation character in it is not a
// connection that mysteriously fails to parse.
func dataSource(c Connection) (string, string, error) {
	if c.Family == "postgres" {
		return "pgx", pgDSN(c), nil
	}
	cfg := mysql.NewConfig()
	cfg.User = c.User
	cfg.Passwd = c.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
	cfg.Timeout = netTimeout
	cfg.ReadTimeout = netTimeout
	cfg.WriteTimeout = netTimeout
	cfg.AllowNativePasswords = true
	name, err := mysqlTLS(c)
	if err != nil {
		return "", "", err
	}
	cfg.TLSConfig = name
	return "mysql", cfg.FormatDSN(), nil
}

// mysqlTLS registers this connection's TLS settings with the driver and returns
// the name the DSN refers to them by. The driver takes a registered name rather
// than a config, so verify-ca means registering one per connection.
func mysqlTLS(c Connection) (string, error) {
	switch c.TLSMode {
	case TLSOff:
		return "", nil
	case TLSRequire:
		// Encrypted, unverified: what a provider that gives no CA certificate
		// can offer, and still better than plaintext across a public network.
		return "skip-verify", nil
	}
	pool, err := caPool(c)
	if err != nil {
		return "", err
	}
	name := "servlo-" + c.Name
	if err := mysql.RegisterTLSConfig(name, verifyCAConfig(pool)); err != nil {
		return "", err
	}
	return name, nil
}

// verifyCAConfig verifies the chain and not the name.
//
// A managed provider's certificate is issued to the cluster, and the hostname
// an operator is given is frequently not the one in it: a private endpoint, a
// CNAME, or a connection pooler in front. verify-ca is the mode the providers
// document for exactly that reason, so the chain is checked against the CA the
// provider handed over and the name is not.
func verifyCAConfig(pool *x509.CertPool) *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // the chain is verified below; the name deliberately is not
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("the server presented no certificate")
			}
			certs := make([]*x509.Certificate, 0, len(rawCerts))
			for _, raw := range rawCerts {
				cert, err := x509.ParseCertificate(raw)
				if err != nil {
					return err
				}
				certs = append(certs, cert)
			}
			opts := x509.VerifyOptions{Roots: pool, Intermediates: x509.NewCertPool()}
			for _, cert := range certs[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err := certs[0].Verify(opts)
			return err
		},
		MinVersion: tls.VersionTLS12,
	}
}

// caPool is the connection's CA certificate, read from where SaveCACert put it.
func caPool(c Connection) (*x509.CertPool, error) {
	data, err := os.ReadFile(c.CACert)
	if err != nil {
		return nil, fmt.Errorf("connection %q verifies the server against a CA certificate that cannot be read: %w", c.Name, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("connection %q: %s holds no PEM certificate", c.Name, c.CACert)
	}
	return pool, nil
}

// pgDSN is the keyword/value connection string. Values are quoted because a
// generated password can contain anything the charset allows.
func pgDSN(c Connection) string {
	pairs := [][2]string{
		{"host", c.Host},
		{"port", fmt.Sprint(c.Port)},
		{"user", c.User},
		{"password", c.Password},
		{"connect_timeout", fmt.Sprint(max(1, int(netTimeout.Seconds())))},
		{"sslmode", pgSSLMode(c)},
	}
	if c.TLSMode == TLSVerifyCA && c.CACert != "" {
		pairs = append(pairs, [2]string{"sslrootcert", c.CACert})
	}
	parts := make([]string, 0, len(pairs))
	for _, kv := range pairs {
		parts = append(parts, kv[0]+"="+pgQuote(kv[1]))
	}
	return strings.Join(parts, " ")
}

// pgSSLMode maps servlo's three modes onto libpq's. verify-ca is libpq's own
// name for the same thing: chain checked, hostname not.
func pgSSLMode(c Connection) string {
	switch c.TLSMode {
	case TLSRequire:
		return "require"
	case TLSVerifyCA:
		return "verify-ca"
	default:
		return "prefer"
	}
}

func pgQuote(v string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

// pgIdent and myIdent quote an identifier that has already been checked against
// the identifier pattern, so the quoting is belt to that braces rather than the
// thing keeping injection out.
func pgIdent(name string) string { return `"` + name + `"` }

func pgLiteral(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }

func myIdent(name string) string { return "'" + name + "'" }

func myIdentBacktick(name string) string { return "`" + name + "`" }

func myLiteral(v string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

// connectionError turns a driver failure into the sentence an operator needs.
func connectionError(c Connection, err error) error {
	if err == nil {
		return nil
	}
	where := net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
	detail := redactPassword(err.Error(), c.Password)

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Errorf("%s does not resolve: %s", c.Host, detail)
	}

	var authErr error
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		switch myErr.Number {
		case 1044, 1045, 1698, 1130:
			authErr = err
		}
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && strings.HasPrefix(pgErr.Code, "28") {
		authErr = err
	}
	if authErr != nil {
		return fmt.Errorf("%s refused the credentials for user %q: %s", where, c.User, detail)
	}

	if isTLSError(err) {
		hint := "the server's certificate did not verify against the CA certificate stored for this connection"
		if c.TLSMode != TLSVerifyCA {
			hint = "the TLS handshake failed"
		}
		return fmt.Errorf("TLS to %s failed: %s: %s", where, hint, detail)
	}

	if isUnreachable(err) {
		return fmt.Errorf("cannot reach %s: %s. If this is a managed database, add this server's public IP to the provider's trusted sources and check the port", where, detail)
	}

	return fmt.Errorf("reached %s, and it answered: %s", where, detail)
}

// isTLSError reports whether the handshake is what failed. The certificate
// errors are typed; the driver-side ones are not, so the prefix the standard
// library puts on them is the signal.
func isTLSError(err error) bool {
	var verifyErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	if errors.As(err, &verifyErr) || errors.As(err, &unknownAuthority) ||
		errors.As(err, &invalid) || errors.As(err, &hostname) {
		return true
	}
	text := err.Error()
	return strings.Contains(text, "tls:") || strings.Contains(text, "x509:") ||
		strings.Contains(text, "TLS requested but server does not support TLS")
}

// isUnreachable reports whether nothing answered. A refused connection, a
// timeout and a cancelled context all mean the operator's next move is the
// network rather than the credentials.
func isUnreachable(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// pgx wraps its dial failures in a type of its own, which carries no net
	// error to match on once the message is all that is left.
	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) {
		return !isTLSError(err) && !strings.Contains(err.Error(), "SQLSTATE")
	}
	text := err.Error()
	return strings.Contains(text, "connection refused") ||
		strings.Contains(text, "i/o timeout") ||
		strings.Contains(text, "invalid connection") ||
		strings.Contains(text, "unexpected EOF") ||
		strings.Contains(text, "driver: bad connection")
}

// redactPassword keeps a driver that echoes its connection string out of the
// panel, the audit log and every screenshot of either.
func redactPassword(text, password string) string {
	if len(password) < 4 {
		return text
	}
	return strings.ReplaceAll(text, password, "****")
}

func isMySQLCode(err error, number uint16) bool {
	var myErr *mysql.MySQLError
	return errors.As(err, &myErr) && myErr.Number == number
}

func isPgCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

// passwordAlphabet is deliberately alphanumeric. The password is pasted into a
// SQL statement, an env file and sometimes a connection string, and a character
// that quotes or comments in any one of those is a bug that only shows up on
// the site unlucky enough to be generated one.
const passwordAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// generatePassword is 32 characters from a crypto source, which is far past
// what a network-reachable database needs and costs nothing.
func generatePassword() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail on Linux; a panic here is better than a
		// predictable database password.
		panic("dbconn: no randomness for a database password: " + err.Error())
	}
	for i, v := range b {
		b[i] = passwordAlphabet[int(v)%len(passwordAlphabet)]
	}
	return string(b)
}
