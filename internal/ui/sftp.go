package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/user"
	"sort"
	"strings"

	"github.com/ServloOfficial/servlo/internal/authz"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/sftpaccess"
)

// SFTP access, from the panel.
//
// Admin rather than site-scoped, and deliberately. Authorising a key gives
// whoever holds it filesystem access as the account every site on this machine
// runs as; until the operator has installed the chroot block it is not confined
// to one site at all. That is not a decision a developer with two assigned
// sites gets to make, so /api/sftp is PermAdmin in the registry.

// SFTPSite is one site's SFTP state.
type SFTPSite struct {
	Domain string `json:"domain"`
	Path   string `json:"path"`
	Port   int    `json:"port"`
	// Confined is whether sshd is actually chrooting this site, which is only
	// true once the operator has run the printed block.
	Confined bool              `json:"confined"`
	Keys     []sftpaccess.Key  `json:"keys"`
	Chroot   sftpaccess.Chroot `json:"chroot"`
}

// SFTPStatus is everything the panel's SFTP page renders.
type SFTPStatus struct {
	Sites []SFTPSite `json:"sites"`
	// User is the Linux account every session logs in as, which is the same one
	// for every site and is the thing the docs are careful about.
	User string `json:"user"`
	// Installed is whether servlo's sshd drop-in is in place at all.
	Installed bool `json:"installed"`
	// Drifted is whether it is in place but no longer matches what servlo would
	// generate, which is what happens when a site is added after the block was
	// installed.
	Drifted bool `json:"drifted"`
	// StagedPath is the file the operator installs, so the printed command has
	// something they can read first.
	StagedPath string `json:"staged_path"`
	// Commands is the sudo block, already prefixed, because servlo prints
	// privileged work and never runs it.
	Commands []string `json:"commands"`
	Error    string   `json:"error,omitempty"`
}

// sftpKeyRequest is a key being authorised.
type sftpKeyRequest struct {
	Domain string `json:"domain"`
	Label  string `json:"label"`
	Key    string `json:"key"`
}

func handleSFTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		status, err := sftpStatus()
		if err != nil {
			writeJSON(w, SFTPStatus{Error: err.Error()})
			return
		}
		writeJSON(w, status)
	case http.MethodPost:
		handleSFTPAddKey(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleSFTPAddKey(w http.ResponseWriter, r *http.Request) {
	var req sftpKeyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, filesResponse{Error: "that request could not be read: " + err.Error()})
		return
	}
	authz.SetAuditDetail(r, "authorised an SFTP key for "+req.Domain+" labelled "+req.Label)

	site, err := config.FindSiteByDomain(req.Domain)
	if err != nil {
		writeJSON(w, filesResponse{Error: "no site called " + req.Domain})
		return
	}
	if _, err := sftpaccess.AssignPort(req.Domain); err != nil {
		writeJSON(w, filesResponse{Error: err.Error()})
		return
	}
	if _, err := sftpaccess.Add(req.Domain, site.Path, req.Label, req.Key); err != nil {
		writeJSON(w, filesResponse{Error: err.Error()})
		return
	}
	writeJSON(w, filesResponse{OK: true, Path: req.Domain})
}

// handleSFTPKey serves DELETE /api/sftp/keys/{fingerprint}.
func handleSFTPKey(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/sftp/")
	name, fingerprint, ok := strings.Cut(rest, "/")
	if !ok || name != "keys" || fingerprint == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authz.SetAuditDetail(r, "withdrew the SFTP key "+fingerprint)
	removed, err := sftpaccess.Remove(fingerprint)
	if err != nil {
		writeJSON(w, filesResponse{Error: err.Error()})
		return
	}
	if !removed {
		writeJSON(w, filesResponse{Error: "no key with that fingerprint is authorised"})
		return
	}
	writeJSON(w, filesResponse{OK: true})
}

// sftpStatus gathers what the panel needs, and stages the configuration so the
// printed command names a file that exists.
func sftpStatus() (SFTPStatus, error) {
	keys, err := sftpaccess.List()
	if err != nil {
		return SFTPStatus{}, err
	}
	ports, err := sftpaccess.Ports()
	if err != nil {
		return SFTPStatus{}, err
	}

	bySite := map[string][]sftpaccess.Key{}
	for _, key := range keys {
		bySite[key.Site] = append(bySite[key.Site], key)
	}

	status := SFTPStatus{User: currentUserName()}
	var chroots []sftpaccess.Chroot
	for domain, siteKeys := range bySite {
		site, err := config.FindSiteByDomain(domain)
		if err != nil {
			// A key for a site that has since been removed. Shown rather than
			// hidden: it is still an authorised key on this machine.
			status.Sites = append(status.Sites, SFTPSite{Domain: domain, Keys: siteKeys, Port: ports[domain]})
			continue
		}
		chroot := sftpaccess.Chroot{Domain: domain, SitePath: site.Path, Port: ports[domain]}
		chroots = append(chroots, chroot)
		status.Sites = append(status.Sites, SFTPSite{
			Domain: domain,
			Path:   site.Path,
			Port:   chroot.Port,
			Keys:   siteKeys,
			Chroot: chroot,
		})
	}
	sort.Slice(chroots, func(i, j int) bool { return chroots[i].Domain < chroots[j].Domain })
	sort.Slice(status.Sites, func(i, j int) bool { return status.Sites[i].Domain < status.Sites[j].Domain })

	keepPorts := sftpaccess.ConfiguredSSHPorts()
	staged, err := sftpaccess.Stage(chroots, keepPorts)
	if err != nil {
		return SFTPStatus{}, err
	}
	status.StagedPath = staged

	wanted := sftpaccess.Config(chroots, keepPorts)
	installed, readErr := os.ReadFile(sftpaccess.SSHDDropIn)
	status.Installed = readErr == nil
	status.Drifted = status.Installed && string(installed) != wanted
	confined := status.Installed && !status.Drifted
	for i := range status.Sites {
		status.Sites[i].Confined = confined && status.Sites[i].Path != ""
	}

	name, group := currentUserGroup()
	status.Commands = sftpaccess.ChrootPlan(chroots, staged, name, group).ForHuman()
	return status, nil
}

// currentUserName is the account sessions log in as. Every site runs as it,
// which is the sentence the docs are built around.
func currentUserName() string {
	name, _ := currentUserGroup()
	return name
}

func currentUserGroup() (string, string) {
	u, err := user.Current()
	if err != nil {
		return "servlo", "servlo"
	}
	group := u.Username
	if g, err := user.LookupGroupId(u.Gid); err == nil {
		group = g.Name
	}
	return u.Username, group
}
