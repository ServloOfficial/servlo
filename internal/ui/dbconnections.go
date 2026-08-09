package ui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/serviceops"
)

// The connections a site's database can be on, and which site is on which.
//
// Admin throughout, including the read: a connection carries where an
// organisation's data lives and the account that administers it, which is not
// something a Developer needs to see to deploy their own site.

// connectionResponse is one connection as the panel sees it. The password is
// never in it: the Connection type drops it from JSON, and nothing here puts it
// back. A panel that can display a credential is a panel that will display it
// over somebody's shoulder.
type connectionResponse struct {
	dbconn.Connection
	Default bool     `json:"default"`
	Sites   []string `json:"sites"`
}

type connectionsResponse struct {
	Connections []connectionResponse `json:"connections"`
	// Services are the local database services installed here, which is what
	// the "add a local connection" form can offer.
	Services []string `json:"services"`
	Error    string   `json:"error,omitempty"`
}

func handleDBConnections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, listConnections())
	case http.MethodPost:
		handleDBConnectionAction(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func listConnections() connectionsResponse {
	out := connectionsResponse{Connections: []connectionResponse{}, Services: []string{}}
	reg, err := dbconn.LoadRegistry()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	onConnection := sitesByConnection(reg.Default)
	for _, c := range reg.Connections {
		sites := onConnection[c.Name]
		if sites == nil {
			sites = []string{}
		}
		// Resolved rather than as stored, so a local connection shows the
		// address it actually answers on rather than a blank row.
		if resolved, err := dbconn.Named(c.Name); err == nil {
			c.Host, c.Port, c.User = resolved.Host, resolved.Port, resolved.User
		}
		out.Connections = append(out.Connections, connectionResponse{
			Connection: c,
			Default:    c.Name == reg.Default,
			Sites:      sites,
		})
	}
	for _, name := range installedDBEngines() {
		out.Services = append(out.Services, name)
	}
	return out
}

// sitesByConnection maps a connection name to the domains of the sites on it.
//
// A site that names nothing is on the default, and it is counted there. The
// alternative reads as "0 sites" beside the connection every site on the
// machine is actually using, which is worse than useless: it is the number an
// operator would check before deciding a connection is safe to remove.
func sitesByConnection(defaultName string) map[string][]string {
	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}
	on := map[string][]string{}
	for _, s := range reg.Sites {
		name := s.Database
		if name == "" {
			name = defaultName
		}
		if name == "" {
			continue
		}
		on[name] = append(on[name], s.PrimaryDomain())
	}
	return on
}

type connectionRequest struct {
	Action string `json:"action"`
	Name   string `json:"name"`
	// A local connection names a service; a managed one names where it is.
	Service  string `json:"service"`
	Engine   string `json:"engine"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	TLSMode  string `json:"tls_mode"`
	CACert   string `json:"ca_cert"`
	// Domain and Connection carry an assignment: which site moves to which.
	Domain     string `json:"domain"`
	Connection string `json:"connection"`
}

func handleDBConnectionAction(w http.ResponseWriter, r *http.Request) {
	var body connectionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "invalid JSON: " + err.Error()})
		return
	}

	var err error
	switch body.Action {
	case "add":
		err = addConnection(body)
	case "remove":
		err = removeConnection(body.Name)
	case "default":
		err = defaultConnection(body.Name)
	case "assign":
		err = assignConnection(body.Domain, body.Connection)
	default:
		writeJSON(w, map[string]any{"error": "unknown action: " + body.Action})
		return
	}
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "connections": listConnections()})
}

func addConnection(body connectionRequest) error {
	reg, err := dbconn.LoadRegistry()
	if err != nil {
		return err
	}
	c := dbconn.LocalConnection(body.Name, body.Service)
	if body.Service == "" {
		c = dbconn.External(body.Name, body.Engine, body.Host, body.Port, body.User, body.Password)
		c.TLSMode, c.CACert = body.TLSMode, body.CACert
	} else if !serviceops.ServiceInstalled(body.Service) {
		return errServiceNotInstalled(body.Service)
	}
	if err := reg.Add(c); err != nil {
		return err
	}
	return dbconn.SaveRegistry(reg)
}

func errServiceNotInstalled(service string) error {
	return &serviceNotInstalledError{service: service}
}

type serviceNotInstalledError struct{ service string }

func (e *serviceNotInstalledError) Error() string {
	return e.service + " is not installed here, so nothing would answer on it"
}

func removeConnection(name string) error {
	reg, err := dbconn.LoadRegistry()
	if err != nil {
		return err
	}
	// Not out from under the sites on it: their env still points at that
	// database, and the connection they name would resolve to nothing.
	if sites := sitesByConnection(reg.Default)[name]; len(sites) > 0 {
		return &connectionInUseError{name: name, sites: sites}
	}
	if err := reg.Remove(name); err != nil {
		return err
	}
	return dbconn.SaveRegistry(reg)
}

type connectionInUseError struct {
	name  string
	sites []string
}

func (e *connectionInUseError) Error() string {
	return "these sites are on " + e.name + ": " + strings.Join(e.sites, ", ") + ". Move them first."
}

func defaultConnection(name string) error {
	reg, err := dbconn.LoadRegistry()
	if err != nil {
		return err
	}
	if err := reg.SetDefault(name); err != nil {
		return err
	}
	return dbconn.SaveRegistry(reg)
}

// assignConnection puts a site on a connection.
//
// It changes where the site's next `servlo env` writes, and nothing else: no
// data is copied and no database is created. Moving the data is `db:move`, and
// conflating the two would make a dropdown that silently migrates a production
// database.
func assignConnection(domain, connection string) error {
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		return err
	}
	if connection != "" {
		if _, err := dbconn.Named(connection); err != nil {
			return err
		}
	}
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	for i := range reg.Sites {
		if reg.Sites[i].Name == site.Name {
			reg.Sites[i].Database = connection
		}
	}
	return config.SaveSites(reg)
}
