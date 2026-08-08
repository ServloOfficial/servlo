package ui

import (
	"encoding/json"
	"net/http"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/siteops"
)

// SitePHPSettingsRequest is the panel's PHP settings form.
//
// Three fields, not five: max upload size is one number that becomes
// upload_max_filesize, post_max_size and client_max_body_size, and max
// execution time is one number that becomes max_execution_time and both
// fastcgi timeouts. Never expose half of one of these pairs (CLAUDE.md §3.4),
// which starts with not offering the halves separately here.
type SitePHPSettingsRequest struct {
	MaxUploadMB         int `json:"max_upload_mb"`
	MaxExecutionSeconds int `json:"max_execution_seconds"`
	MemoryLimitMB       int `json:"memory_limit_mb"`
}

// SitePHPSettingsResponse is what the form reads back on GET.
type SitePHPSettingsResponse struct {
	MaxUploadMB         int `json:"max_upload_mb"`
	MaxExecutionSeconds int `json:"max_execution_seconds"`
	MemoryLimitMB       int `json:"memory_limit_mb"`
	// The ceilings, so the form can refuse a value before the round trip and
	// show the same limit the server enforces rather than a second copy of it.
	MaxUploadCeilingMB   int `json:"max_upload_ceiling_mb"`
	MaxExecutionCeilingS int `json:"max_execution_ceiling_s"`
	MemoryLimitCeilingMB int `json:"memory_limit_ceiling_mb"`
}

// SiteNginxSettingsRequest is the panel's nginx settings form.
type SiteNginxSettingsRequest struct {
	StaticCacheDays   int                     `json:"static_cache_days"`
	ResponseHeaders   []config.ResponseHeader `json:"response_headers"`
	CanonicalHost     string                  `json:"canonical_host"`
	RedirectTo        string                  `json:"redirect_to"`
	RedirectPermanent bool                    `json:"redirect_permanent"`
	Redirects         []config.Redirect       `json:"redirects"`
}

// SiteNginxSettingsResponse is what that form reads back on GET.
type SiteNginxSettingsResponse struct {
	StaticCacheDays        int                     `json:"static_cache_days"`
	ResponseHeaders        []config.ResponseHeader `json:"response_headers"`
	StaticCacheCeilingDays int                     `json:"static_cache_ceiling_days"`
	CanonicalHost          string                  `json:"canonical_host"`
	// CanonicalAvailable is false when the site does not serve a domain and its
	// own www form, which is every site the toggle cannot apply to. The form
	// explains that rather than offering a choice that would be refused.
	CanonicalAvailable bool   `json:"canonical_available"`
	ApexHost           string `json:"apex_host"`
	WWWHost            string `json:"www_host"`

	RedirectTo        string            `json:"redirect_to"`
	RedirectPermanent bool              `json:"redirect_permanent"`
	Redirects         []config.Redirect `json:"redirects"`
}

// handleSiteNginxSettings serves GET and POST on
// /api/sites/{domain}/nginx-settings.
func handleSiteNginxSettings(w http.ResponseWriter, r *http.Request, site *config.Site) {
	switch r.Method {
	case http.MethodGet:
		apex, www, available := site.WWWPair()
		redirects := site.Redirects
		if redirects == nil {
			redirects = []config.Redirect{}
		}
		headers := site.ResponseHeaders
		if headers == nil {
			// An empty list rather than null, so the form iterates it without
			// having to know that JSON has two ways to say nothing.
			headers = []config.ResponseHeader{}
		}
		writeJSON(w, SiteNginxSettingsResponse{
			StaticCacheDays:        site.StaticCacheDays,
			ResponseHeaders:        headers,
			StaticCacheCeilingDays: config.StaticCacheCeilingDays,
			CanonicalHost:          site.CanonicalHost,
			CanonicalAvailable:     available,
			ApexHost:               apex,
			WWWHost:                www,
			RedirectTo:             site.RedirectTo,
			RedirectPermanent:      site.RedirectPermanent,
			Redirects:              redirects,
		})
	case http.MethodPost:
		var req SiteNginxSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, SiteActionResponse{Error: "reading the settings: " + err.Error()})
			return
		}
		if err := siteops.SetSiteNginxSettings(site, siteops.NginxSettings{
			StaticCacheDays:   req.StaticCacheDays,
			ResponseHeaders:   req.ResponseHeaders,
			CanonicalHost:     req.CanonicalHost,
			RedirectTo:        req.RedirectTo,
			RedirectPermanent: req.RedirectPermanent,
			Redirects:         req.Redirects,
		}); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSitePHPSettings serves GET and POST on
// /api/sites/{domain}/php-settings.
func handleSitePHPSettings(w http.ResponseWriter, r *http.Request, site *config.Site) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, SitePHPSettingsResponse{
			MaxUploadMB:          site.MaxUploadMB,
			MaxExecutionSeconds:  site.MaxExecutionSeconds,
			MemoryLimitMB:        site.MemoryLimitMB,
			MaxUploadCeilingMB:   config.MaxUploadCeilingMB,
			MaxExecutionCeilingS: config.MaxExecutionCeilingS,
			MemoryLimitCeilingMB: config.MemoryLimitCeilingMB,
		})
	case http.MethodPost:
		var req SitePHPSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, SiteActionResponse{Error: "reading the settings: " + err.Error()})
			return
		}
		// The whole set every time, so a form that omits a field is saying it
		// should be cleared rather than leaving the site holding a value
		// nobody chose.
		if err := siteops.SetSitePHPSettings(site, siteops.PHPSettings{
			MaxUploadMB:         req.MaxUploadMB,
			MaxExecutionSeconds: req.MaxExecutionSeconds,
			MemoryLimitMB:       req.MemoryLimitMB,
		}); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
