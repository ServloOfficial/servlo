package ui

import (
	"encoding/json"
	"net/http"

	"github.com/realrashid/servlo/internal/authz"
	"github.com/realrashid/servlo/internal/serverguard"
	"github.com/realrashid/servlo/internal/sshkeys"
)

// The security panel surface.
//
//	GET  /api/security       → the audit, the firewall plan, fail2ban, the keys
//	POST /api/security/keys  → authorise a key, or take one away
//
// Only one of those changes anything, and that is the whole shape of this
// screen: the firewall and fail2ban need root, so servlo reports them and hands
// over commands, while authorized_keys belongs to this account, so servlo edits
// it. The view says which is which rather than presenting a row of buttons half
// of which quietly do nothing.

// SecurityResponse is everything the security view renders.
type SecurityResponse struct {
	Findings []serverguard.Finding `json:"findings"`
	Firewall FirewallPlan          `json:"firewall"`
	Fail2ban serverguard.Fail2ban  `json:"fail2ban"`
	Provider serverguard.Provider  `json:"provider"`
	Keys     []sshkeys.Key         `json:"keys"`
	KeysPath string                `json:"keys_path"`
	// SSHPort is what the plan was built for, so a server whose sshd is not on
	// 22 can see that the plan would lock it out and say so.
	SSHPort int    `json:"ssh_port"`
	Error   string `json:"error,omitempty"`
}

// FirewallPlan is the ufw configuration to paste.
type FirewallPlan struct {
	Why      string   `json:"why"`
	Commands []string `json:"commands"`
	Open     []int    `json:"open"`
	// Unexpected are the listening ports the plan does not account for, which
	// is the finding worth acting on: a rule is a claim, an open socket is what
	// is true.
	Unexpected []int `json:"unexpected"`
}

// KeyRequest adds or removes one authorised key.
type KeyRequest struct {
	Action string `json:"action"`
	// Key is the whole public key line, for an add.
	Key string `json:"key,omitempty"`
	// Name is who it belongs to.
	Name string `json:"name,omitempty"`
	// Fingerprint names the key to remove.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// KeyResponse is what came of it, with the list as it now stands so the panel
// redraws without a second request.
type KeyResponse struct {
	OK    bool          `json:"ok"`
	Keys  []sshkeys.Key `json:"keys"`
	Error string        `json:"error,omitempty"`
}

// defaultSSHPort is what the firewall plan assumes. Reading sshd_config would
// be better and servlo cannot: it is root-owned on Ubuntu, and guessing wrong
// in the direction of "closed" is the guess that locks somebody out.
const defaultSSHPort = 22

func handleSecurity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	want := serverguard.DefaultWant(defaultSSHPort)
	plan := serverguard.FirewallPlan(want)

	resp := SecurityResponse{
		Findings: serverguard.Audit(want),
		Firewall: FirewallPlan{Why: plan.Why, Commands: plan.Commands, Open: []int{}, Unexpected: []int{}},
		Fail2ban: serverguard.Fail2banStatus(serverguard.DefaultJail),
		Provider: serverguard.DetectProvider(),
		Keys:     []sshkeys.Key{},
		KeysPath: sshkeys.Path(),
		SSHPort:  defaultSSHPort,
	}
	if open, err := serverguard.Listening(); err == nil {
		resp.Firewall.Open = open
		if extra := serverguard.UnexpectedlyOpen(want, open); extra != nil {
			resp.Firewall.Unexpected = extra
		}
	}
	if keys, err := sshkeys.List(); err != nil {
		resp.Error = err.Error()
	} else if keys != nil {
		resp.Keys = keys
	}
	writeJSON(w, resp)
}

func handleSecurityKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var req KeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, KeyResponse{Keys: []sshkeys.Key{}, Error: "reading the request: " + err.Error()})
		return
	}

	switch req.Action {
	case "add":
		key, err := sshkeys.Add(req.Key, req.Name)
		if err != nil {
			writeJSON(w, KeyResponse{Keys: currentKeys(), Error: err.Error()})
			return
		}
		// The fingerprint, never the key material: the audit log is read by
		// people and kept for a long time, and the fingerprint is what
		// identifies the key anyway.
		authz.SetAuditDetail(r, "authorised the SSH key "+key.Comment+" ("+key.Fingerprint+")")
	case "remove":
		removed, err := sshkeys.Remove(req.Fingerprint)
		if err != nil {
			writeJSON(w, KeyResponse{Keys: currentKeys(), Error: err.Error()})
			return
		}
		if !removed {
			writeJSON(w, KeyResponse{Keys: currentKeys(), Error: "that key is not authorised on this server"})
			return
		}
		authz.SetAuditDetail(r, "removed the SSH key "+req.Fingerprint)
	default:
		writeJSON(w, KeyResponse{Keys: currentKeys(), Error: "unknown action " + req.Action})
		return
	}
	writeJSON(w, KeyResponse{OK: true, Keys: currentKeys()})
}

func currentKeys() []sshkeys.Key {
	keys, err := sshkeys.List()
	if err != nil || keys == nil {
		return []sshkeys.Key{}
	}
	return keys
}
