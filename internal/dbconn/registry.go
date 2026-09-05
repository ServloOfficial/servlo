package dbconn

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"gopkg.in/yaml.v3"
)

// The connection registry.
//
// A site's database is named rather than assumed. Before this, every code path
// that wanted a database looked for a container called servlo-mysql, which is
// true right up until the operator puts their data on a managed database and
// then it is false everywhere at once. A named connection is the seam: the site
// says which one it uses, this says where that one is, and nothing in between
// has to know whether the answer is a container on this machine.
//
// An install with no registry behaves exactly as it did, because a name servlo
// does not recognise falls back to the local service of that name. Nobody has
// to migrate anything to keep working, and the file appears the first time
// somebody adds a connection worth writing down.

// TLS modes an external connection may ask for. Managed providers differ on
// which they require, and a provider's own console is where an operator reads
// that, so servlo carries the choice rather than deciding it.
const (
	TLSOff      = ""
	TLSRequire  = "require"
	TLSVerifyCA = "verify-ca"
)

// Registry is the set of connections this install knows, and which one a new
// site gets.
type Registry struct {
	// Default names the connection a site with no choice of its own uses.
	Default string `yaml:"default,omitempty"`
	// Connections are the named connections, in the order they were added.
	Connections []Connection `yaml:"connections,omitempty"`
}

// registryFile is where the registry lives. 0600 and outside any site
// directory, because an external connection's credentials are in it.
func registryFile() string {
	return filepath.Join(config.ConfigDir(), "databases.yaml")
}

// connectionName is what a connection may be called: something short that can
// be a key in config, a path segment and a value in a site's registry entry.
var connectionName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)

// LoadRegistry reads the registry, returning an empty one when there is no
// file. Absent is the ordinary state of a fresh install, not an error.
func LoadRegistry() (*Registry, error) {
	data, err := os.ReadFile(registryFile())
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{}, nil
		}
		return nil, fmt.Errorf("reading the database connections: %w", err)
	}
	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parsing the database connections: %w", err)
	}
	return &reg, nil
}

// SaveRegistry writes the registry 0600.
func SaveRegistry(reg *Registry) error {
	data, err := yaml.Marshal(reg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.ConfigDir(), 0700); err != nil {
		return err
	}
	// Written 0600 from the start rather than chmodded after, so an external
	// database's password is never briefly world-readable.
	return os.WriteFile(registryFile(), data, 0600)
}

// Find returns the named connection from reg, and whether it is there.
func (reg *Registry) Find(name string) (Connection, bool) {
	for _, c := range reg.Connections {
		if c.Name == name {
			return c, true
		}
	}
	return Connection{}, false
}

// Named resolves a connection by name, ready to use.
//
// A name the registry does not carry resolves to the local service of that
// name, which is what keeps every site created before connections existed
// working: their database is "mysql", and "mysql" is a service servlo runs.
func Named(name string) (Connection, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Default()
	}
	reg, err := LoadRegistry()
	if err != nil {
		return Connection{}, err
	}
	if c, ok := reg.Find(name); ok {
		return c.resolved()
	}
	return ForService(name)
}

// Default is the connection a site gets when it has not chosen one.
//
// The registry's default when it names something, and the local MySQL service
// otherwise, which is the database an install has had since before any of this
// was configurable.
func Default() (Connection, error) {
	reg, err := LoadRegistry()
	if err != nil {
		return Connection{}, err
	}
	if reg.Default != "" {
		if c, ok := reg.Find(reg.Default); ok {
			return c.resolved()
		}
		// A default naming a connection that has been removed is a broken
		// install rather than a reason to quietly put a site somewhere else.
		return Connection{}, fmt.Errorf("the default database connection %q is not configured", reg.Default)
	}
	return ForService("mysql")
}

// resolved fills in what a stored connection leaves out.
//
// A local connection stores only which service it is: its address and
// credentials are properties of this install, and writing them down would mean
// a rotated password lived on in a second file. An external one is stored whole,
// because nothing here can derive it.
func (c Connection) resolved() (Connection, error) {
	if !c.Local() {
		return c, nil
	}
	local, err := ForService(c.Service)
	if err != nil {
		return c, err
	}
	local.Name = c.Name
	return local, nil
}

// Add puts a connection in the registry, as the default when it is the first
// one. Adding the first connection to an install that has been running on the
// local MySQL all along does not silently move anything: existing sites name
// their own database and keep it.
func (reg *Registry) Add(c Connection) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if _, exists := reg.Find(c.Name); exists {
		return fmt.Errorf("a database connection called %q already exists", c.Name)
	}
	reg.Connections = append(reg.Connections, c)
	if reg.Default == "" {
		reg.Default = c.Name
	}
	return nil
}

// Remove drops a connection. Refused while it is the default and something else
// could be, because an install whose default names nothing cannot create a site.
func (reg *Registry) Remove(name string) error {
	idx := -1
	for i, c := range reg.Connections {
		if c.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("there is no database connection called %q", name)
	}
	reg.Connections = append(reg.Connections[:idx], reg.Connections[idx+1:]...)
	if reg.Default == name {
		reg.Default = ""
		if len(reg.Connections) > 0 {
			reg.Default = reg.Connections[0].Name
		}
	}
	return nil
}

// SetDefault points new sites at a connection.
func (reg *Registry) SetDefault(name string) error {
	if _, ok := reg.Find(name); !ok {
		return fmt.Errorf("there is no database connection called %q", name)
	}
	reg.Default = name
	return nil
}

// Validate reports whether a connection is complete enough to reach a database.
//
// The point of checking here rather than at the first query is that a
// half-configured connection is discovered when somebody deploys, and by then
// the site exists and its .env is written.
func (c Connection) Validate() error {
	switch {
	case !connectionName.MatchString(c.Name):
		return fmt.Errorf("%q is not a usable connection name: lowercase letters, digits and dashes, up to 40 characters", c.Name)
	case Dialect(c.Family) == "":
		return fmt.Errorf("connection %q: %q is not a database servlo can manage, which is mysql, mariadb or postgres", c.Name, c.Family)
	}
	if c.Local() {
		return nil
	}
	switch {
	case strings.TrimSpace(c.Host) == "":
		return fmt.Errorf("connection %q needs the host its database answers on", c.Name)
	case c.Port <= 0 || c.Port > 65535:
		return fmt.Errorf("connection %q needs a port between 1 and 65535, not %d", c.Name, c.Port)
	case strings.TrimSpace(c.User) == "":
		return fmt.Errorf("connection %q needs the user servlo creates databases as", c.Name)
	case c.Password == "":
		return fmt.Errorf("connection %q needs that user's password", c.Name)
	case c.TLSMode != TLSOff && c.TLSMode != TLSRequire && c.TLSMode != TLSVerifyCA:
		return fmt.Errorf("connection %q asks for TLS mode %q, which is none, require or verify-ca", c.Name, c.TLSMode)
	case c.TLSMode == TLSVerifyCA && strings.TrimSpace(c.CACert) == "":
		return fmt.Errorf("connection %q verifies the server's certificate but carries no CA certificate to verify it against", c.Name)
	}
	return nil
}

// External builds a connection to a database servlo does not run. The port
// defaults to the engine's, since a managed provider that moved it says so and
// one that did not should not have to be told.
func External(name, family, host string, port int, user, password string) Connection {
	if port == 0 {
		port = familyPort(family)
	}
	return Connection{
		Name:     name,
		Family:   Dialect(family),
		Host:     strings.TrimSpace(host),
		Port:     port,
		User:     strings.TrimSpace(user),
		Password: password,
	}
}

// LocalConnection builds a connection to a service servlo runs.
func LocalConnection(name, service string) Connection {
	return Connection{Name: name, Family: DialectForService(service), Service: service}
}
