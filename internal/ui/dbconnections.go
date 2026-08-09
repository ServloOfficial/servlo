package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/dnscheck"
	"github.com/realrashid/servlo/internal/serviceops"
)

// Reaching a managed database and working out this server's own address are the
// two things here that touch the network, so they are the two seams: the
// panel's behaviour around them is worth testing without either.
var (
	testConnection = dbconn.Test
	serverIPs      = func() ([]string, error) { return dnscheck.ThisServerStrings(context.Background()) }
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
	// ServerIPs are this server's public addresses, which is what a managed
	// provider's trusted-sources list has to hold before anything here can reach
	// it. It travels with the list because the operator needs it before they save
	// a connection, not after it fails.
	ServerIPs      []string `json:"server_ips"`
	ServerIPsError string   `json:"server_ips_error,omitempty"`
	Error          string   `json:"error,omitempty"`
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
	out := connectionsResponse{Connections: []connectionResponse{}, Services: []string{}, ServerIPs: []string{}}
	if ips, err := serverIPs(); err != nil {
		out.ServerIPsError = err.Error()
	} else {
		out.ServerIPs = ips
	}
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
	// CACertPEM is the provider's certificate as uploaded. Servlo stores it
	// itself rather than keeping a path into somebody's home directory or, worse,
	// into a site tree that nginx serves and a deploy can delete.
	CACertPEM string `json:"ca_cert_pem"`
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
	case "test":
		err = testSavedConnection(body.Name)
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
		if strings.TrimSpace(body.CACertPEM) != "" {
			path, err := dbconn.SaveCACert(body.Name, []byte(body.CACertPEM))
			if err != nil {
				return err
			}
			c.CACert = path
		}
	} else if !serviceops.ServiceInstalled(body.Service) {
		return errServiceNotInstalled(body.Service)
	}
	if err := reg.Add(c); err != nil {
		return err
	}
	// Reached before it is written down. A connection saved and found to be
	// unreachable at the first deploy is a site whose env file points at a
	// database nothing here can open, discovered by the person deploying it.
	if !c.Local() {
		if err := testConnection(c); err != nil {
			// The certificate belongs to a connection that was never saved.
			_ = dbconn.RemoveCACert(body.Name)
			return err
		}
	}
	return dbconn.SaveRegistry(reg)
}

// testSavedConnection reaches a connection that is already configured, for the
// Test button on a row. The reason to press it is usually that something moved
// on the provider's side, so it re-reads the connection rather than trusting
// what the panel last rendered.
func testSavedConnection(name string) error {
	c, err := dbconn.Named(name)
	if err != nil {
		return err
	}
	return testConnection(c)
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
	if err := dbconn.SaveRegistry(reg); err != nil {
		return err
	}
	// The certificate and the accounts servlo created on it go too: kept, they
	// are credentials for a server nothing points at, and the next connection to
	// take that name would inherit them.
	if err := dbconn.RemoveCACert(name); err != nil {
		return err
	}
	return dbconn.ForgetSiteUsers(name)
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
