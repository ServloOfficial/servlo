package serviceops

import (
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
)

// Every database this install can reach, for an admin UI's server list.
//
// discover_family answers a different question: which containers of this family
// are up. That was the whole answer while every database servlo knew about was
// one it ran. It stops being the answer the moment a site's data is on a managed
// server, because there is no container to find and the site's own phpMyAdmin
// would show every database except the one the site is actually using.
//
// So the list is the connection registry first, in the order the operator added
// them, and then the local services of those families that no connection names,
// which is what keeps an install that has never configured a connection showing
// exactly what it shows today.

// DatabaseConnectionsFor lists the databases of the given families: registry
// connections, local and managed, plus the local services not already named by
// one. A local database that is not running is left out, the same way
// discover_family leaves it out, because an admin UI listing a server it cannot
// open is a login screen with no explanation.
func DatabaseConnectionsFor(families []string) []config.DBConnectionInfo {
	dialects := map[string]bool{}
	for _, f := range families {
		if d := dbconn.Dialect(f); d != "" {
			dialects[d] = true
		}
	}
	if len(dialects) == 0 {
		return nil
	}

	var out []config.DBConnectionInfo
	claimed := map[string]bool{}

	if reg, err := dbconn.LoadRegistry(); err == nil {
		for _, stored := range reg.Connections {
			if !dialects[dbconn.Dialect(stored.Family)] {
				continue
			}
			c, err := dbconn.Named(stored.Name)
			if err != nil {
				continue
			}
			if c.Local() {
				if !serviceIsRunning(c.Service) {
					continue
				}
				claimed[c.Service] = true
			}
			out = append(out, connectionInfo(c))
		}
	}

	for _, family := range families {
		for _, host := range config.ServicesInFamily(family) {
			service := strings.TrimPrefix(host, "servlo-")
			if claimed[service] {
				continue
			}
			c, err := dbconn.ForService(service)
			if err != nil {
				continue
			}
			claimed[service] = true
			out = append(out, connectionInfo(c))
		}
	}
	return out
}

// connectionInfo is a connection as the config layer needs it, named the way an
// admin UI should label it: the connection's own name, or the service standing
// in for an unnamed local one.
func connectionInfo(c dbconn.Connection) config.DBConnectionInfo {
	name := c.Name
	if name == "" {
		name = c.Service
	}
	return config.DBConnectionInfo{
		Name:     name,
		Family:   c.Family,
		Local:    c.Local(),
		Host:     c.Host,
		Port:     c.Port,
		User:     c.User,
		Password: c.Password,
		TLSMode:  c.TLSMode,
		CACert:   c.CACert,
	}
}
