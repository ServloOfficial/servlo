// Package webhook authenticates a git host's push notification and decides
// whether it should deploy.
//
// Three questions, in order, and each one is a refusal on its own:
//
//   - Did this really come from the repository? A signature over the raw body,
//     keyed by the site's own secret. Nothing else about the request is
//     evidence: the source address belongs to a host with a large and changing
//     range, and a shared token in a header is a token in every proxy log.
//   - Is this the branch that deploys? A push to a feature branch reaching
//     production is the failure mode this whole filter exists for.
//   - Have we already run this one? A signed body stays valid forever, so
//     anything that captured one can send it again and again.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/realrashid/servlo/internal/config"
)

// SignatureHeader is what GitHub and Gitea send. Named here because the
// handler and the panel's instructions both have to agree on it.
const SignatureHeader = "X-Hub-Signature-256"

// DeliveryHeader identifies one delivery attempt, and is what makes a replay
// recognisable.
const DeliveryHeader = "X-GitHub-Delivery"

// Verify checks the signature over the exact bytes received.
//
// Over the raw body, not a re-encoding of the parsed payload: any parse and
// re-serialise changes whitespace and key order, and the signature is over what
// was sent.
func Verify(secret string, body []byte, header string) error {
	if secret == "" {
		return fmt.Errorf("this site has no webhook secret")
	}
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return fmt.Errorf("the request carries no %s signature", SignatureHeader)
	}
	sent, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return fmt.Errorf("the signature is not hex")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	// Constant time. A comparison that stops at the first wrong byte tells an
	// attacker how much of a guess was right, and enough of those is the
	// signature.
	if !hmac.Equal(sent, mac.Sum(nil)) {
		return fmt.Errorf("the signature does not match this site's secret")
	}
	return nil
}

// Push is the part of a push payload servlo acts on.
type Push struct {
	// Branch is empty for anything that is not a push to a branch, which is
	// how a tag push and a branch deletion arrive.
	Branch string
	Commit string
	// Subject is the head commit's message, used only to say what a webhook
	// deploy was for.
	Subject string
}

type pushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	HeadCommit *struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"head_commit"`
}

// ParsePush reads a push payload.
func ParsePush(body []byte) (Push, error) {
	var p pushPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return Push{}, fmt.Errorf("this does not look like a push payload: %w", err)
	}

	var out Push
	const branchPrefix = "refs/heads/"
	if strings.HasPrefix(p.Ref, branchPrefix) {
		out.Branch = strings.TrimPrefix(p.Ref, branchPrefix)
	}
	out.Commit = p.After
	if p.HeadCommit != nil {
		if p.HeadCommit.ID != "" {
			out.Commit = p.HeadCommit.ID
		}
		out.Subject = strings.SplitN(p.HeadCommit.Message, "\n", 2)[0]
	}
	return out, nil
}

// ShouldDeploy reports whether a push to pushed should deploy a site whose
// filter is configured.
//
// An empty filter means any branch. That is a real choice an operator can make,
// and the panel makes them make it rather than defaulting to it. A push that
// names no branch never deploys, whatever the filter says: a tag or a deletion
// is not a new state of a branch to put live.
func ShouldDeploy(configured, pushed string) bool {
	if pushed == "" {
		return false
	}
	return configured == "" || configured == pushed
}

// DeliveryLimit is how many delivery ids a site remembers.
//
// Enough to cover a host retrying a delivery, which is the case this catches in
// practice, without keeping a file that grows for the life of the install.
const DeliveryLimit = 200

var (
	deliveryMu   sync.Mutex
	siteFileName = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,62}[a-zA-Z0-9])?$`)
)

// DeliveryDir holds one file of seen delivery ids per site.
func DeliveryDir() string {
	return filepath.Join(config.DataDir(), "webhook-deliveries")
}

func deliveryPath(site string) (string, error) {
	if !siteFileName.MatchString(site) {
		return "", fmt.Errorf("%q is not a usable site name", site)
	}
	return filepath.Join(DeliveryDir(), site+".json"), nil
}

func readDeliveries(site string) ([]string, error) {
	path, err := deliveryPath(site)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		// A corrupt file means no replay protection, which is not something to
		// carry on past quietly.
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return ids, nil
}

// Seen records a delivery and reports whether it had already arrived.
//
// Read from disk every time, with no in-memory copy in front of it. A cache
// here would be a second answer to "has this arrived", and the two disagree in
// exactly the cases that matter: a restart empties one and not the other, and a
// failed write leaves a delivery remembered in a process that is about to exit.
// A push is not frequent enough for a couple of hundred short strings to be
// worth the second source of truth.
func Seen(site, delivery string) (bool, error) {
	if strings.TrimSpace(delivery) == "" {
		// Refused rather than waved through. Without an id there is nothing to
		// recognise a replay by, and a signed body is valid forever.
		return false, fmt.Errorf("the request carries no %s header, so a replay could not be told from a new push", DeliveryHeader)
	}

	deliveryMu.Lock()
	defer deliveryMu.Unlock()

	ids, err := readDeliveries(site)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == delivery {
			return true, nil
		}
	}

	ids = append(ids, delivery)
	if len(ids) > DeliveryLimit {
		ids = ids[len(ids)-DeliveryLimit:]
	}

	path, err := deliveryPath(site)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return false, err
	}
	return false, nil
}
