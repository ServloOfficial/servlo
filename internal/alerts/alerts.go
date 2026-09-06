// Package alerts is the list of things wrong with this server.
//
// The panel is where an alert lives, and email is a copy of it. That order
// matters: panel SMTP is optional, a mail server can be down, and an alert that
// only ever existed as an email nobody received is exactly the failure this is
// supposed to prevent. So an alert is written down first and sent second, and a
// send that fails is reported without losing the alert.
//
// The same failure repeating does not repeat the alert. A site that has been
// down for a day is one thing wrong, not two hundred and eighty-eight, and an
// inbox that gets flooded is an inbox that starts filtering servlo out.
package alerts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/mailsend"
)

// What can be wrong. Each is one line in the panel and one subject in an inbox.
const (
	KindSiteDown         = "site_down"
	KindCertRenewFailed  = "cert_renew_failed"
	KindBackupFailed     = "backup_failed"
	KindBackupUnverified = "backup_unverified"
	KindDeployFailed     = "deploy_failed"
	KindWorkerDown       = "worker_down"
	KindDiskFilling      = "disk_filling"
)

// storeFile holds the open alerts. In the data directory rather than the config
// one, because this is state servlo produced rather than anything an operator
// configured.
const storeFile = "alerts.json"

var storeMu sync.Mutex

// Alert is one thing wrong.
type Alert struct {
	Kind string `json:"kind"`
	// Site is empty for something wrong with the server rather than with a
	// site, which is what keeps a disk alert from looking like a site's.
	Site    string    `json:"site,omitempty"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// key is what makes two raises the same alert: the same thing wrong with the
// same site. The message is deliberately not part of it, because a failure that
// reworded itself between attempts is still the same failure.
func (a Alert) key() string { return a.Kind + "\x00" + a.Site }

// send is the seam. Production emails through the panel's SMTP account; a test
// asserts on what would have gone out.
var send = func(a Alert) error { return mailsend.SendPanelAlert(a.Subject(), a.Body()) }

// Subject is what an inbox shows. The site is in it, because an operator with
// twenty sites reads the subject and nothing else.
func (a Alert) Subject() string {
	if a.Site != "" {
		return fmt.Sprintf("[servlo] %s: %s", a.Site, title(a.Kind))
	}
	return fmt.Sprintf("[servlo] %s", title(a.Kind))
}

// Body is the whole alert, because an email that says only that something is
// wrong makes somebody open the panel to find out what.
func (a Alert) Body() string {
	where := a.Site
	if where == "" {
		where = "this server"
	}
	return fmt.Sprintf("%s\n\n%s\n\nNoticed at %s.\n",
		title(a.Kind)+" on "+where, a.Message, a.At.Format(time.RFC1123))
}

// Title is the heading for this alert's kind, so the panel and an inbox say the
// same words and there is one place to change them.
func (a Alert) Title() string { return title(a.Kind) }

func title(kind string) string {
	switch kind {
	case KindSiteDown:
		return "Site is down"
	case KindCertRenewFailed:
		return "Certificate renewal failed"
	case KindBackupFailed:
		return "Backup failed"
	case KindBackupUnverified:
		return "A backup could not be restored"
	case KindDeployFailed:
		return "Deploy failed"
	case KindWorkerDown:
		return "A worker is down"
	case KindDiskFilling:
		return "Disk is filling up"
	}
	return kind
}

func storePath() string { return filepath.Join(config.DataDir(), storeFile) }

// Raise records an alert and sends it, unless the same thing is already wrong.
func Raise(a Alert) error {
	if a.At.IsZero() {
		a.At = time.Now().UTC()
	}
	storeMu.Lock()
	open, err := load()
	if err != nil {
		storeMu.Unlock()
		return err
	}
	if _, already := open[a.key()]; already {
		// Already reported and not yet recovered. Nothing has changed, so
		// nothing is written and nothing is sent.
		storeMu.Unlock()
		return nil
	}
	open[a.key()] = a
	err = save(open)
	storeMu.Unlock()
	if err != nil {
		return err
	}
	// Sent after it is recorded, so a mail failure loses nothing.
	return send(a)
}

// Clear takes an alert away because the thing it is about recovered.
//
// This is what makes the list mean anything. An alert that stays after a site
// came back teaches an operator to ignore the list, which costs more than the
// alert was ever worth.
func Clear(kind, site string) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	open, err := load()
	if err != nil {
		return err
	}
	k := Alert{Kind: kind, Site: site}.key()
	if _, found := open[k]; !found {
		return nil
	}
	delete(open, k)
	return save(open)
}

// List is everything currently wrong, newest first.
func List() ([]Alert, error) {
	storeMu.Lock()
	defer storeMu.Unlock()

	open, err := load()
	if err != nil {
		return nil, err
	}
	out := make([]Alert, 0, len(open))
	for _, a := range open {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

func load() (map[string]Alert, error) {
	raw, err := os.ReadFile(storePath())
	if os.IsNotExist(err) {
		return map[string]Alert{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the alerts: %w", err)
	}
	var list []Alert
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parsing the alerts: %w", err)
	}
	open := make(map[string]Alert, len(list))
	for _, a := range list {
		open[a.key()] = a
	}
	return open, nil
}

func save(open map[string]Alert) error {
	list := make([]Alert, 0, len(open))
	for _, a := range open {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].At.After(list[j].At) })
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.DataDir(), 0700); err != nil {
		return err
	}
	// 0600: this is a list of what is wrong with a production server, which is
	// not something other accounts on it need.
	return os.WriteFile(storePath(), raw, 0600)
}
