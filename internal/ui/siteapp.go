package ui

import (
	"encoding/json"
	"net/http"

	"github.com/ServloOfficial/servlo/internal/appinstall"
	"github.com/ServloOfficial/servlo/internal/appstore"
)

// Adding a site by installing an application.
//
// The fourth source, alongside an existing folder, a clone and a ZIP upload.
// Two requests: one to ask what the store offers, so the form can show it, and
// one to install.
//
// The steps themselves live in internal/appinstall, which the CLI drives too.
// A panel that reimplemented the order would be a second chance to get it
// wrong, and the order is the part that decides what a failed install leaves
// behind.

// installApp is the whole install, as a seam. Every step below it needs a
// network, a container and a registry, so a handler test that could not replace
// it could only run on a server.
var installApp = appinstall.Install

// AppRow is one app as the Add Site form needs it.
type AppRow struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Version     string `json:"version"`
	// NeedsDatabase tells the form whether to say a database will be created.
	NeedsDatabase bool `json:"needs_database"`
	// SelfSetup is true for an application whose own installer servlo cannot
	// drive, so the form can say where the install stops before it starts.
	SelfSetup bool `json:"self_setup"`
}

// SiteAppRequest is what the form sends.
type SiteAppRequest struct {
	App        string `json:"app"`
	Domain     string `json:"domain"`
	Path       string `json:"path"`
	Connection string `json:"connection"`
	AdminUser  string `json:"admin_user"`
	AdminEmail string `json:"admin_email"`
	SiteTitle  string `json:"site_title"`
}

// SiteAppResponse is what happened, including the credentials that exist
// nowhere else.
type SiteAppResponse struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Site   string `json:"site,omitempty"`
	Domain string `json:"domain,omitempty"`
	Path   string `json:"path,omitempty"`
	// AdminUser and AdminPassword are shown once and never fetched again. They
	// are not written to the audit log, which records that an app was installed
	// and on what, never what opens it.
	AdminUser     string `json:"admin_user,omitempty"`
	AdminPassword string `json:"admin_password,omitempty"`
	Database      string `json:"database,omitempty"`
	// Note is what is left for the operator to do, for an application whose own
	// installer servlo cannot complete.
	Note string `json:"note,omitempty"`
}

// handleApps answers GET /api/apps with what the store offers.
func handleApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	apps, err := appstore.List()
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	rows := make([]AppRow, 0, len(apps))
	for _, app := range apps {
		rows = append(rows, AppRow{
			Name:          app.Name,
			Label:         app.Label,
			Description:   app.Description,
			Version:       app.Source.Version,
			NeedsDatabase: app.Database.Required,
			SelfSetup:     !app.Setup.Declared(),
		})
	}
	writeJSON(w, map[string]any{"apps": rows})
}

// handleSiteApp answers POST /api/sites/app by installing one.
func handleSiteApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req SiteAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, SiteAppResponse{Error: "invalid request body"})
		return
	}

	res, err := installApp(r.Context(), appinstall.Options{
		App:        req.App,
		Domain:     req.Domain,
		Path:       req.Path,
		Connection: req.Connection,
		AdminUser:  req.AdminUser,
		AdminEmail: req.AdminEmail,
		SiteTitle:  req.SiteTitle,
	})
	if err != nil {
		writeJSON(w, SiteAppResponse{Error: err.Error()})
		return
	}

	writeJSON(w, SiteAppResponse{
		OK:            true,
		Site:          res.Site.Name,
		Domain:        res.Site.PrimaryDomain(),
		Path:          res.Site.Path,
		AdminUser:     res.AdminUser,
		AdminPassword: res.AdminPassword,
		Database:      res.Database.Name,
		Note:          res.Note,
	})
}
