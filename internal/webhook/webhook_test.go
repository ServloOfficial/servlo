package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

const push = `{"ref":"refs/heads/main","after":"a1b2c3d4","head_commit":{"id":"a1b2c3d4","message":"add the orders index"}}`

// The signature is the whole authentication. Nothing else about the request
// proves who sent it.
func TestVerify_AcceptsAGoodSignature(t *testing.T) {
	if err := Verify("s3cret", []byte(push), sign("s3cret", push)); err != nil {
		t.Fatalf("a correctly signed body was refused: %v", err)
	}
}

func TestVerify_RefusesTampering(t *testing.T) {
	good := sign("s3cret", push)

	cases := map[string]struct{ secret, body, header string }{
		"a body changed after signing": {"s3cret", strings.Replace(push, "main", "evil", 1), good},
		"the wrong secret":             {"other", push, good},
		"no signature at all":          {"s3cret", push, ""},
		"a signature that is not hex":  {"s3cret", push, "sha256=zzzz"},
		"the wrong algorithm":          {"s3cret", push, strings.Replace(good, "sha256=", "sha1=", 1)},
		"no algorithm prefix":          {"s3cret", push, strings.TrimPrefix(good, "sha256=")},
		"an empty secret":              {"", push, sign("", push)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Verify(c.secret, []byte(c.body), c.header); err == nil {
				t.Error("accepted")
			}
		})
	}
}

// Compared in constant time. A byte-by-byte comparison leaks how much of a
// guess was right, which over enough attempts is the signature.
func TestVerify_UsesAConstantTimeComparison(t *testing.T) {
	src, err := os.ReadFile("webhook.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "hmac.Equal") {
		t.Error("the signature is not compared with hmac.Equal")
	}
}

func TestParsePush_ReadsTheBranchAndCommit(t *testing.T) {
	got, err := ParsePush([]byte(push))
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != "main" {
		t.Errorf("Branch = %q, want main", got.Branch)
	}
	if got.Commit != "a1b2c3d4" {
		t.Errorf("Commit = %q", got.Commit)
	}
}

// A tag push carries refs/tags/..., and a branch delete carries a ref with no
// commit. Neither is a push to a branch and neither should deploy.
func TestParsePush_NonBranchRefs(t *testing.T) {
	for _, body := range []string{
		`{"ref":"refs/tags/v1.2.0","after":"a1b2c3d4"}`,
		`{"ref":"","after":"a1b2c3d4"}`,
	} {
		got, err := ParsePush([]byte(body))
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if got.Branch != "" {
			t.Errorf("%s: Branch = %q, want none", body, got.Branch)
		}
	}
}

func TestParsePush_RefusesRubbish(t *testing.T) {
	if _, err := ParsePush([]byte("not json")); err == nil {
		t.Error("a body that is not JSON was parsed")
	}
}

// A branch filter that matched nothing would deploy every branch, and one that
// matched everything would deploy a feature branch to production.
func TestShouldDeploy(t *testing.T) {
	cases := []struct {
		configured, pushed string
		want               bool
	}{
		{"main", "main", true},
		{"main", "feature/checkout", false},
		{"main", "", false},
		// Empty means any branch, which the panel makes an explicit choice.
		{"", "main", true},
		{"", "feature/checkout", true},
		{"", "", false},
		// Branch names are case sensitive in git, and so is this.
		{"main", "Main", false},
	}
	for _, c := range cases {
		if got := ShouldDeploy(c.configured, c.pushed); got != c.want {
			t.Errorf("ShouldDeploy(%q, %q) = %v, want %v", c.configured, c.pushed, got, c.want)
		}
	}
}

func deliveriesHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
}

// A replayed delivery is a deploy nobody asked for, and a signed body stays
// valid forever: anything that captured one can send it again.
func TestSeen_RefusesAReplay(t *testing.T) {
	deliveriesHome(t)

	first, err := Seen("shop", "delivery-1")
	if err != nil {
		t.Fatal(err)
	}
	if first {
		t.Error("a delivery nobody had sent was reported as already seen")
	}

	again, err := Seen("shop", "delivery-1")
	if err != nil {
		t.Fatal(err)
	}
	if !again {
		t.Error("the same delivery was accepted twice")
	}
}

// Remembered on disk, not only in the process, because a restart that forgets
// is a restart during which every captured delivery works again. Asserted
// against the file rather than a second call, so an in-memory answer could not
// make this pass.
func TestSeen_IsRememberedOnDisk(t *testing.T) {
	deliveriesHome(t)
	if _, err := Seen("shop", "delivery-1"); err != nil {
		t.Fatal(err)
	}

	ids, err := readDeliveries("shop")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "delivery-1" {
		t.Errorf("on disk = %v, want the delivery that arrived", ids)
	}
}

// Per site. Two sites pushed by the same repository share delivery ids, and one
// site's deploy must not swallow the other's.
func TestSeen_IsPerSite(t *testing.T) {
	deliveriesHome(t)
	if _, err := Seen("shop", "delivery-1"); err != nil {
		t.Fatal(err)
	}

	got, err := Seen("blog", "delivery-1")
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("one site's delivery blocked another site's")
	}
}

// The list is bounded, or a site deploying on every push keeps a file that
// grows forever.
func TestSeen_IsBounded(t *testing.T) {
	deliveriesHome(t)
	for i := 0; i < DeliveryLimit+40; i++ {
		if _, err := Seen("shop", string(rune('a'+i%26))+string(rune('0'+i/26))); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := readDeliveries("shop")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) > DeliveryLimit {
		t.Errorf("kept %d delivery ids, want at most %d", len(ids), DeliveryLimit)
	}
}

// A sender that provides no delivery id gets no replay protection, and that is
// refused rather than waved through: the alternative is a signed body that can
// be sent forever.
func TestSeen_RequiresADeliveryID(t *testing.T) {
	deliveriesHome(t)

	if _, err := Seen("shop", ""); err == nil {
		t.Error("an empty delivery id was accepted")
	}
}

// The site name decides the filename, so one that could climb out is refused.
func TestSeen_RefusesAnUnusableSiteName(t *testing.T) {
	deliveriesHome(t)

	if _, err := Seen("../etc", "delivery-1"); err == nil {
		t.Error("a site name that escapes the directory was accepted")
	}
}

func TestDeliveries_NotWorldReadable(t *testing.T) {
	deliveriesHome(t)
	if _, err := Seen("shop", "delivery-1"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(DeliveryDir(), "shop.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %04o, want 0600", perm)
	}
}
