package ui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ServloOfficial/servlo/internal/authz"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/sitesmtp"
)

// The site's outgoing mail, in the panel.
//
// Servlo runs no mail server (CLAUDE.md §3.7), so a site that sends mail sends
// it through a provider, and this card is where that account is entered. Saving
// stores it 0600 and writes it into whichever env keys the site's framework
// declares; the test button proves it before a customer's password reset is the
// thing that finds out it was wrong.
//
// The password goes in and never comes back. The form is told only that one is
// stored, which is what it needs to offer "leave blank to keep it".

// smtpStatus is what the site's Mail card reads.
type smtpStatus struct {
	// Configured is whether this site has an account at all, which is the
	// difference between the empty form and the filled one.
	Configured bool                `json:"configured"`
	Settings   config.RedactedSMTP `json:"settings"`
	// EnvKeys are the keys in the site's env file a save rewrites, named so the
	// operator can see what is about to change. Empty when the framework's
	// definition says nothing about mail, in which case Note explains it.
	EnvKeys []string `json:"env_keys,omitempty"`
	Note    string   `json:"note,omitempty"`
	// EnvFile is the file those keys land in: .env for most, wp-config.php for
	// WordPress, app/etc/env.php for Magento.
	EnvFile string `json:"env_file,omitempty"`
}

// smtpRoute serves /api/sites/{domain}/smtp and its test action.
func smtpRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) == 0 || rest[0] != "smtp" {
		return false
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	switch {
	case len(rest) == 1 && r.Method == http.MethodGet:
		writeJSON(w, siteSMTPStatus(site))
	case len(rest) == 1 && r.Method == http.MethodPost:
		handleSMTPSave(w, r, site)
	case len(rest) == 1 && r.Method == http.MethodDelete:
		handleSMTPDelete(w, r, site)
	case len(rest) == 2 && rest[1] == "test" && r.Method == http.MethodPost:
		handleSMTPTest(w, r, site)
	default:
		http.NotFound(w, r)
	}
	return true
}

func siteSMTPStatus(site *config.Site) smtpStatus {
	status := smtpStatus{}
	if acct, ok, err := config.SiteSMTP(site.Name); err == nil && ok {
		status.Configured = acct.Configured()
		status.Settings = acct.Redacted()
	}
	if fw, ok := config.GetFrameworkForDir(site.Framework, site.Path); ok && fw.HasEnvConfig() {
		file, _ := fw.Env.Resolve(site.Path)
		status.EnvFile = file
	}
	// Named from the framework definition alone, so the empty form already says
	// what a save would change.
	keys, err := sitesmtp.EnvKeys(site)
	if err != nil {
		status.Note = err.Error()
		return status
	}
	status.EnvKeys = keys
	return status
}

func handleSMTPSave(w http.ResponseWriter, r *http.Request, site *config.Site) {
	settings, err := decodeSMTPBody(r, w)
	if err != nil {
		writeSMTPError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	// Validated against the stored password rather than the empty one the form
	// sends when it is keeping it, so a save that changes only the sender name
	// is not refused for a credential it was never given.
	check := settings
	if check.Password == "" {
		if stored, ok, err := config.SiteSMTP(site.Name); err == nil && ok {
			check.Password = stored.Password
		}
	}
	if err := check.Validate(); err != nil {
		writeSMTPError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.SaveSiteSMTP(site.Name, settings); err != nil {
		writeSMTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	authz.SetAuditDetail(r, "set the SMTP settings for "+site.Name+" to "+settings.Host)

	keys, wroteErr := sitesmtp.WriteEnv(site)
	if wroteErr != nil {
		// Stored but not wired. Worth being loud about: the panel now holds
		// settings the application is not using, and nothing else would say so.
		writeJSON(w, map[string]any{
			"ok":      true,
			"warning": "The settings are saved but could not be written into the site's env file: " + wroteErr.Error(),
		})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "env_keys": keys})
}

func handleSMTPDelete(w http.ResponseWriter, r *http.Request, site *config.Site) {
	if err := config.DeleteSiteSMTP(site.Name); err != nil {
		writeSMTPError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The env file keeps whatever was written into it. Emptying it would leave
	// the application with no transport at all mid-request; forgetting the
	// panel's copy of the credential is what was asked for.
	authz.SetAuditDetail(r, "removed the SMTP settings for "+site.Name)
	writeJSON(w, map[string]any{"ok": true})
}

func handleSMTPTest(w http.ResponseWriter, r *http.Request, site *config.Site) {
	var body struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeSMTPError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	// An empty box sends to the account's own sender address, the same as the
	// panel's test does. Refusing it instead would be a dead end on the one
	// press an operator makes right after filling the form in.
	to := strings.TrimSpace(body.To)
	if to == "" {
		acct, ok, err := config.SiteSMTP(site.Name)
		if err != nil || !ok || !acct.Configured() {
			writeSMTPError(w, http.StatusBadRequest, site.Name+" has no SMTP settings yet")
			return
		}
		to = acct.FromAddress
	}
	if err := sitesmtp.SendTest(site, to); err != nil {
		authz.SetAuditDetail(r, "sent a test email for "+site.Name+", which the mail server refused")
		writeSMTPError(w, http.StatusBadGateway, err.Error())
		return
	}
	authz.SetAuditDetail(r, "sent a test email for "+site.Name+" to "+to)
	writeJSON(w, map[string]any{"ok": true, "to": to})
}

func writeSMTPError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}
