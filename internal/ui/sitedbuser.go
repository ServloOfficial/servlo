package ui

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/ServloOfficial/servlo/internal/authz"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbcred"
	"github.com/ServloOfficial/servlo/internal/dbuser"
	"github.com/ServloOfficial/servlo/internal/sitetpl"
)

// The site's own database account, in the panel.
//
// Two things an operator needs and cannot get anywhere else: which account this
// site reaches its database as, and a way to change its password without
// opening a SQL shell. The password itself is never in either answer. It is in
// the site's env file, which is where the application reads it from, and a
// panel that can display a credential is a panel that will display it over
// somebody's shoulder.

// dbUserStatus is what the site's Database card reads.
type dbUserStatus struct {
	// Connection and Location are which database this is and where it answers,
	// so the card can say "on the managed one" rather than just "the database".
	Connection string `json:"connection"`
	Location   string `json:"location"`
	Database   string `json:"database"`
	// User is the account the site reaches the database as, and OwnAccount
	// distinguishes its own from the administrator it falls back to.
	User       string `json:"user"`
	OwnAccount bool   `json:"own_account"`
	AdminUser  string `json:"admin_user"`
	// EnvKeys are the keys in the site's env file a rotation rewrites, so the
	// card can name them before anybody presses the button.
	EnvKeys []string `json:"env_keys,omitempty"`
	// Note explains why a rotation cannot be offered, when it cannot.
	Note string `json:"note,omitempty"`
}

// dbUserRoute serves /api/sites/{domain}/db-user and its rotate action.
func dbUserRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) == 0 || rest[0] != "db-user" {
		return false
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	switch {
	case len(rest) == 1 && r.Method == http.MethodGet:
		handleDBUserStatus(w, site)
	case len(rest) == 2 && rest[1] == "rotate" && r.Method == http.MethodPost:
		handleDBUserRotate(w, r, site)
	default:
		http.NotFound(w, r)
	}
	return true
}

func handleDBUserStatus(w http.ResponseWriter, site *config.Site) {
	status, err := siteDBUserStatus(site)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, status)
}

// siteDBUserStatus reads the account without touching the database: it is the
// answer to a page load, and a page load that opens a connection to a managed
// server three regions away is a page load that hangs.
func siteDBUserStatus(site *config.Site) (dbUserStatus, error) {
	conn, err := dbconn.Named(site.Database)
	if err != nil {
		return dbUserStatus{}, err
	}
	database := sitetpl.DBName(site.Path)
	status := dbUserStatus{
		Connection: dbcred.Key(conn),
		Location:   dbuser.Address(conn),
		Database:   database,
		User:       conn.User,
		AdminUser:  conn.User,
	}
	if cred, ok := dbcred.For(conn, database); ok {
		status.User = cred.User
		status.OwnAccount = true
	}
	if keys, err := dbuser.EnvKeys(site); err == nil {
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		status.EnvKeys = names
	} else {
		status.Note = err.Error()
	}
	return status, nil
}

func handleDBUserRotate(w http.ResponseWriter, r *http.Request, site *config.Site) {
	conn, err := dbconn.Named(site.Database)
	if err != nil {
		writeDBUserError(w, r, err)
		return
	}
	database := sitetpl.DBName(site.Path)
	cred, err := dbuser.Rotate(conn, database)
	if err != nil {
		writeDBUserError(w, r, err)
		return
	}
	authz.SetAuditDetail(r, "rotated the database password for "+database+" on "+dbcred.Key(conn))

	keys, err := dbuser.WriteEnv(site)
	if err != nil {
		// The server has the new password and the env file does not. That is
		// the one outcome worth being loud about, because the site will fail to
		// connect at its next reload and nothing else would say why.
		authz.SetAuditDetail(r, "rotated the database password for "+database+" but could not write the env file")
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]any{
			"error": "The new password is set on the database server but could not be written into the site's env file: " +
				err.Error() + " Set it there yourself, or the site will fail to connect at its next reload.",
			"user": cred.User,
		})
		return
	}
	sort.Strings(keys)
	writeJSON(w, map[string]any{
		"ok":       true,
		"user":     cred.User,
		"env_keys": keys,
	})
}

func writeDBUserError(w http.ResponseWriter, _ *http.Request, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
}
