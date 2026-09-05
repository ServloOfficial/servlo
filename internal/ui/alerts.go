package ui

import (
	"encoding/json"
	"net/http"

	"github.com/ServloOfficial/servlo/internal/alerts"
	"github.com/ServloOfficial/servlo/internal/authz"
)

// The alerts panel surface.
//
//	GET  /api/alerts  → everything currently wrong, newest first
//	POST /api/alerts  → take one off the list by hand
//
// Dismissing changes nothing about the server, which is the whole reason it is
// safe to offer. If the failure is still happening the next check raises it
// again, so the button is for the alert servlo cannot see the end of rather
// than a way to make a problem go away.

// AlertsResponse is the list, with a shape the panel can render without
// knowing what any kind means.
type AlertsResponse struct {
	Alerts []PanelAlert `json:"alerts"`
	Error  string       `json:"error,omitempty"`
}

// PanelAlert is one alert with its heading already resolved, so the kind stays
// the server's vocabulary and the panel does not carry a second copy of it that
// can drift.
type PanelAlert struct {
	alerts.Alert
	Title string `json:"title"`
}

// AlertDismissRequest names the one to take away.
type AlertDismissRequest struct {
	Kind string `json:"kind"`
	Site string `json:"site,omitempty"`
}

func handleAlerts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleAlertsList(w)
	case http.MethodPost:
		handleAlertDismiss(w, r)
	default:
		http.NotFound(w, r)
	}
}

func handleAlertsList(w http.ResponseWriter) {
	open, err := alerts.List()
	if err != nil {
		writeJSON(w, AlertsResponse{Alerts: []PanelAlert{}, Error: err.Error()})
		return
	}
	out := make([]PanelAlert, 0, len(open))
	for _, a := range open {
		out = append(out, PanelAlert{Alert: a, Title: a.Title()})
	}
	writeJSON(w, AlertsResponse{Alerts: out})
}

func handleAlertDismiss(w http.ResponseWriter, r *http.Request) {
	var req AlertDismissRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, AlertsResponse{Alerts: []PanelAlert{}, Error: "reading the request: " + err.Error()})
		return
	}
	if req.Kind == "" {
		writeJSON(w, AlertsResponse{Alerts: []PanelAlert{}, Error: "which alert?"})
		return
	}
	if err := alerts.Clear(req.Kind, req.Site); err != nil {
		writeJSON(w, AlertsResponse{Alerts: []PanelAlert{}, Error: err.Error()})
		return
	}
	authz.SetAuditDetail(r, "dismissed the "+req.Kind+" alert")
	handleAlertsList(w)
}
