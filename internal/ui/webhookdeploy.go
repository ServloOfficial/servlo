package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/deploy"
	"github.com/realrashid/servlo/internal/webhook"
)

// The one route on the panel that takes bytes from the internet with no
// session behind them.
//
// Everything else is either an asset or sits behind the auth gate. This has to
// answer a git host, which holds no cookie, so the signature is the whole
// authentication and the handler is written as a series of refusals: an
// unreadable body, an unknown endpoint, a bad signature, a branch that is not
// the one configured, a delivery already seen. Only a request that survives all
// of them deploys anything.

// maxWebhookBody caps what is read before anything else happens. A push payload
// with a large commit list is tens of kilobytes; a megabyte is generous. Read
// first and check the size second would be the bug, since the point is to not
// buffer an unbounded body from an unauthenticated caller.
const maxWebhookBody = 1 << 20

// webhookPathPrefix is where the endpoint lives. The last segment is the site's
// public webhook id.
//
// Written out again at the mux registration rather than registered through this
// constant. The surface scan reads route literals out of the source to check
// every one declares a permission, and a route registered through a constant is
// a route it cannot see, which is the exact hole it exists to close. A test
// keeps this and the permission declaration in step.
const webhookPathPrefix = "/api/webhooks/deploy/"

// handleWebhookDeploy answers a git host's push notification.
func handleWebhookDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, webhookPathPrefix)
	name, found, err := config.SiteNameByWebhookID(id)
	if err != nil || !found {
		// Nothing about what was not found. A message distinguishing "no such
		// endpoint" from "that site has it turned off" turns this URL into a
		// way to enumerate which sites exist.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	site, ok := siteByName(name)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	hook, err := config.SiteWebhook(site)
	if err != nil || !hook.Enabled {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil {
		http.Error(w, "could not read the request", http.StatusBadRequest)
		return
	}
	if len(body) > maxWebhookBody {
		http.Error(w, "the payload is too large", http.StatusRequestEntityTooLarge)
		return
	}

	if err := webhook.Verify(hook.Secret, body, r.Header.Get(webhook.SignatureHeader)); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Only now is the sender known, so only now is it worth parsing what they
	// sent or writing anything down about them.
	push, err := webhook.ParsePush(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !webhook.ShouldDeploy(hook.Branch, push.Branch) {
		// 200, deliberately. A git host retries a 5xx and eventually disables a
		// hook that keeps failing, and "this was not the branch" is a correct
		// outcome rather than a failure.
		writeJSON(w, map[string]any{
			"ok": true, "deployed": false,
			"ignored": fmt.Sprintf("this site deploys %s, and the push was to %s",
				branchLabel(hook.Branch), branchLabel(push.Branch)),
		})
		return
	}

	seen, err := webhook.Seen(site.Name, r.Header.Get(webhook.DeliveryHeader))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if seen {
		// Also 200: a retry of something already done is not an error, and
		// answering otherwise makes the host keep trying.
		writeJSON(w, map[string]any{"ok": true, "deployed": false, "ignored": "this delivery has already been handled"})
		return
	}

	release, busyWith, ok := tryAcquireRun(siteRunLockKey(site), "deploy")
	if !ok {
		// 409 rather than 200: this one is worth retrying, and a host that
		// backs off and comes back is exactly the behaviour wanted while the
		// previous deploy finishes.
		http.Error(w, "this site is busy running "+busyWith, http.StatusConflict)
		return
	}
	defer release()

	// Nowhere to stream to. The git host is waiting on a status code, not
	// reading a log, so the output goes to the panel's own log and the outcome
	// goes into the site's deploy history like any other deploy.
	var out strings.Builder
	res, deployErr := runDeployFn(deploy.Defaults(site, &out))
	recordDeploy(r, site, res, deployErr, false)

	if deployErr != nil {
		writeJSON(w, map[string]any{
			"ok": false, "deployed": false,
			"error": deployErr.Error(),
			"from":  res.FromCommit, "to": res.ToCommit,
		})
		return
	}
	writeJSON(w, map[string]any{
		"ok": true, "deployed": true,
		"from": res.FromCommit, "to": res.ToCommit,
		"duration_ms": res.Duration.Milliseconds(),
	})
}

// siteByName finds the registered site an endpoint belongs to.
func siteByName(name string) (*config.Site, bool) {
	reg, err := config.LoadSites()
	if err != nil {
		return nil, false
	}
	for i := range reg.Sites {
		if reg.Sites[i].Name == name {
			return &reg.Sites[i], true
		}
	}
	return nil, false
}

// branchLabel names a branch for a message, including the case where there is
// not one.
func branchLabel(branch string) string {
	if branch == "" {
		return "no branch"
	}
	return branch
}

// SiteWebhookRequest is the panel turning a webhook on or off.
type SiteWebhookRequest struct {
	Enabled bool   `json:"enabled"`
	Branch  string `json:"branch"`
	// Regenerate replaces the secret and keeps the endpoint.
	Regenerate bool `json:"regenerate"`
}

// SiteWebhookResponse is a site's webhook as the panel may see it.
type SiteWebhookResponse struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
	Branch  string `json:"branch,omitempty"`
	// Secret is filled in only on the response that mints it. Reading it back
	// later is not something the panel needs to do, and an endpoint that
	// returned it would put it in every browser cache and screenshot from then
	// on. An operator who lost it regenerates.
	Secret string `json:"secret,omitempty"`
	// SignatureHeader and DeliveryHeader are what the git host must send, so
	// the panel can say so without a second copy of the names.
	SignatureHeader string `json:"signature_header"`
	DeliveryHeader  string `json:"delivery_header"`
}

// handleSiteWebhook serves GET and POST on /api/sites/{domain}/webhook.
func handleSiteWebhook(w http.ResponseWriter, r *http.Request, site *config.Site) {
	switch r.Method {
	case http.MethodGet:
		hook, err := config.SiteWebhook(site)
		if err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, webhookResponse(site, hook, ""))

	case http.MethodPost:
		var req SiteWebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, SiteActionResponse{Error: "reading the request: " + err.Error()})
			return
		}

		if !req.Enabled {
			if err := config.DisableSiteWebhook(site); err != nil {
				writeJSON(w, SiteActionResponse{Error: err.Error()})
				return
			}
			writeJSON(w, webhookResponse(site, config.SiteWebhookConfig{}, ""))
			return
		}

		before, err := config.SiteWebhook(site)
		if err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		hook, err := config.EnableSiteWebhook(site, strings.TrimSpace(req.Branch))
		if err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if req.Regenerate {
			if hook, err = config.RegenerateSiteWebhookSecret(site); err != nil {
				writeJSON(w, SiteActionResponse{Error: err.Error()})
				return
			}
		}

		// The secret goes back only when this request created it: enabling a
		// site that had no webhook, or explicitly regenerating. Returning it on
		// every save would mean a branch change hands it out again.
		secret := ""
		if req.Regenerate || !before.Enabled || before.Secret == "" {
			secret = hook.Secret
		}
		writeJSON(w, webhookResponse(site, hook, secret))

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func webhookResponse(site *config.Site, hook config.SiteWebhookConfig, secret string) SiteWebhookResponse {
	res := SiteWebhookResponse{
		Enabled:         hook.Enabled,
		Branch:          hook.Branch,
		Secret:          secret,
		SignatureHeader: webhook.SignatureHeader,
		DeliveryHeader:  webhook.DeliveryHeader,
	}
	if hook.Enabled && hook.ID != "" {
		res.URL = webhookPathPrefix + hook.ID
	}
	return res
}
