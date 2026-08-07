package ui

import (
	"encoding/json"

	"github.com/realrashid/servlo/internal/authz"
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
