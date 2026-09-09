package ui

import (
	"encoding/json"

	"github.com/ServloOfficial/servlo/internal/authz"
)

// Scoping the websocket.
//
// The broker builds one snapshot and hands it to every connection, which was
// right when every connection was the same operator. With roles it is not: a
// developer who cannot open another team's site must not be able to watch it
// either, or the routes are gated and the door beside them is open.
//
// Filtering happens per connection at send time rather than per role in the
// broker, because the broker does not know who is listening and a cache keyed
// by role would be a cache to invalidate whenever an assignment changed.

// scopeSites filters a serialised sites snapshot down to what a scope may see.
//
// An absent payload stays absent: the client reads a missing key as "unchanged"
// and an empty array as "there are none", so turning one into the other would
// wipe the list on every frame that happened not to carry sites.
func scopeSites(payload []byte, scope authz.Scope) []byte {
	if len(payload) == 0 || !scope.Limited() {
		return payload
	}
	var sites []json.RawMessage
	if err := json.Unmarshal(payload, &sites); err != nil {
		// A payload servlo cannot read is not one to pass through. Doing so
		// would make a malformed frame the way to see everything.
		return []byte("[]")
	}
	kept := make([]json.RawMessage, 0, len(sites))
	for _, raw := range sites {
		var site struct {
			Domain string `json:"domain"`
		}
		if err := json.Unmarshal(raw, &site); err != nil {
			continue
		}
		if scope.MaySee(site.Domain) {
			kept = append(kept, raw)
		}
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return []byte("[]")
	}
	return out
}

// scopeServices blanks the services for anyone who may not administer them.
// Sending them and trusting the browser not to render them would put the
// enforcement in the client.
func scopeServices(payload []byte, scope authz.Scope) []byte {
	if len(payload) == 0 || scope.MayAdminister() {
		return payload
	}
	return []byte("[]")
}

// scopeUnhealthyWorkers blanks worker health for anyone who may not administer.
//
// /api/workers/health is PermAdmin, so a developer asking over HTTP is refused,
// and this is the same answer on the socket beside it. The list is not only the
// fact that a worker is unhealthy: each entry carries the site's name, its
// worker and unit names, and the last line out of its journal, which is another
// team's application output.
//
// Blanked rather than filtered to what the scope may see, deliberately. The
// socket must not answer something the route refuses; widening both together so
// a developer sees their own sites' worker health is a product decision, not a
// scoping one.
func scopeUnhealthyWorkers(payload []byte, scope authz.Scope) []byte {
	if len(payload) == 0 || scope.MayAdminister() {
		return payload
	}
	return []byte("[]")
}

// scopeNotification drops a notification about a site this scope may not see.
//
// A notification names its site in the title, the tag and the data, so
// broadcasting every one to every peer tells a developer that a site they
// cannot open exists, which of its routes is slow and by how much. The site is
// read from the data the notification already carries for the frontend to route
// on; one that names no site is about the server rather than a tenant and goes
// to everyone.
//
// Returns nil for a notification to withhold, which assembleSnapshot treats as
// no notification on this frame.
func scopeNotification(payload []byte, scope authz.Scope) []byte {
	if len(payload) == 0 || !scope.Limited() {
		return payload
	}
	var n struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(payload, &n); err != nil {
		// Unreadable is not a reason to deliver it. A malformed payload must
		// not become the way to reach every connection.
		return nil
	}
	site := n.Data["site"]
	if site == "" {
		return payload
	}
	if !scope.MaySee(site) {
		return nil
	}
	return payload
}
