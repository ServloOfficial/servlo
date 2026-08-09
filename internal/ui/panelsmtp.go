package ui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/realrashid/servlo/internal/authz"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/mailsend"
)

// The panel's own mail account.
//
// Separate from any site's, and deliberately: the panel emails the operator
// about certificates, backups and deploys, while a site emails its customers.
// They are usually different providers and always different senders, and one
// account doing both means a renewal alert arrives from a client's domain.
//
// Alerts are addressed to the account's own sender address. An operator who
// wants them somewhere else points that address at the inbox they read; adding
// a second field for a destination invites the two to disagree, and an alert
// nobody receives is the failure this whole path exists to avoid.

// panelSMTPStatus is what the System → Mail card reads.
type panelSMTPStatus struct {
	Configured bool                `json:"configured"`
	Settings   config.RedactedSMTP `json:"settings"`
}

// handlePanelSMTP serves /api/settings/smtp.
func handlePanelSMTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		status := panelSMTPStatus{}
		if acct, ok, err := config.PanelSMTP(); err == nil && ok {
			status.Configured = acct.Configured()
			status.Settings = acct.Redacted()
		}
		writeJSON(w, status)
	case http.MethodPost:
		handlePanelSMTPSave(w, r)
	case http.MethodDelete:
		if err := config.DeletePanelSMTP(); err != nil {
			writeSMTPError(w, http.StatusInternalServerError, err.Error())
			return
		}
		authz.SetAuditDetail(r, "removed the panel's SMTP settings")
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handlePanelSMTPSave(w http.ResponseWriter, r *http.Request) {
	settings, err := decodeSMTPBody(r, w)
	if err != nil {
		writeSMTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	check := settings
	if check.Password == "" {
		if stored, ok, err := config.PanelSMTP(); err == nil && ok {
			check.Password = stored.Password
		}
	}
	if err := check.Validate(); err != nil {
		writeSMTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.SavePanelSMTP(settings); err != nil {
		writeSMTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	authz.SetAuditDetail(r, "set the panel's SMTP settings to "+settings.Host)
	writeJSON(w, map[string]any{"ok": true})
}

// handlePanelSMTPTest serves /api/settings/smtp/test: one message through the
// panel's own account, so an operator finds out the alert path is broken now
// rather than during the certificate failure it was supposed to warn about.
func handlePanelSMTPTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeSMTPError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	acct, ok, err := config.PanelSMTP()
	if err != nil {
		writeSMTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || !acct.Configured() {
		writeSMTPError(w, http.StatusBadRequest, "the panel has no SMTP settings yet")
		return
	}
	to := strings.TrimSpace(body.To)
	if to == "" {
		to = acct.FromAddress
	}
	if err := mailsend.Send(acct, to,
		"Servlo test message",
		"This is the test message Servlo sends to prove the panel's SMTP settings work. Alerts will arrive this way.",
	); err != nil {
		authz.SetAuditDetail(r, "sent a panel test email, which the mail server refused")
		writeSMTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	authz.SetAuditDetail(r, "sent a panel test email to "+to)
	writeJSON(w, map[string]any{"ok": true, "to": to})
}

// decodeSMTPBody reads the form both mail cards post. One decoder because they
// are the same seven fields, and two would drift.
func decodeSMTPBody(r *http.Request, w http.ResponseWriter) (config.SMTPSettings, error) {
	var body struct {
		Host        string `json:"host"`
		Port        int    `json:"port"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		Encryption  string `json:"encryption"`
		FromAddress string `json:"from_address"`
		FromName    string `json:"from_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		return config.SMTPSettings{}, err
	}
	return config.SMTPSettings{
		Host: strings.TrimSpace(body.Host), Port: body.Port,
		Username: strings.TrimSpace(body.Username), Password: body.Password,
		Encryption:  strings.TrimSpace(body.Encryption),
		FromAddress: strings.TrimSpace(body.FromAddress),
		FromName:    strings.TrimSpace(body.FromName),
	}, nil
}
