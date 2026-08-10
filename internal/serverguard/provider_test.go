package serverguard

import (
	"strings"
	"testing"
)

func answerAs(t *testing.T, paths ...string) {
	t.Helper()
	prev := metadataGet
	t.Cleanup(func() { metadataGet = prev })
	answer := map[string]bool{}
	for _, p := range paths {
		answer[p] = true
	}
	metadataGet = func(path string) (string, bool) {
		if answer[path] {
			return "12345678", true
		}
		return "", false
	}
}

// The point is not the provider's name. It is being told where the rules are,
// which is the difference between an afternoon debugging ufw and thirty
// seconds looking at the right screen.
func TestDetectProvider_NamesWhereTheRulesAre(t *testing.T) {
	answerAs(t, "/metadata/v1/id")

	got := DetectProvider()
	if got.Name != "DigitalOcean" {
		t.Fatalf("detected %q", got.Name)
	}
	if !strings.Contains(got.FirewallURL, "digitalocean.com") {
		t.Errorf("the link does not go to the firewall screen: %q", got.FirewallURL)
	}
	if !strings.Contains(got.Detail, "80") || !strings.Contains(got.Detail, "443") {
		t.Errorf("the advice does not mention the ports a site needs: %q", got.Detail)
	}
}

func TestDetectProvider_KnowsTheOthers(t *testing.T) {
	answerAs(t, "/latest/meta-data/instance-id")
	if got := DetectProvider(); got.Name != "AWS EC2" {
		t.Errorf("EC2 detected as %q", got.Name)
	}

	answerAs(t, "/hetzner/v1/metadata/instance-id")
	if got := DetectProvider(); got.Name != "Hetzner Cloud" {
		t.Errorf("Hetzner detected as %q", got.Name)
	}
}

// A bare-metal server, or one at a provider servlo does not know, still gets
// told that a firewall in front of the machine is the place to look. Saying
// nothing would be worse than saying it might be something servlo cannot see.
func TestDetectProvider_StillSaysWhereToLookWhenItCannotTell(t *testing.T) {
	answerAs(t)

	got := DetectProvider()
	if got.Name != "" {
		t.Errorf("a provider was guessed at: %q", got.Name)
	}
	if !strings.Contains(got.Detail, "ufw") {
		t.Errorf("nothing tells an operator where to look: %q", got.Detail)
	}
}

// A metadata service that answers is not the same as one that answers for this
// provider. Reading a 404 as a droplet would send an operator to the wrong
// console.
func TestDetectProvider_DoesNotGuessFromAnAddressThatMerelyAnswers(t *testing.T) {
	prev := metadataGet
	t.Cleanup(func() { metadataGet = prev })
	metadataGet = func(string) (string, bool) { return "", false }

	if got := DetectProvider(); got.Name != "" {
		t.Errorf("detected %q from a metadata service that answered nothing", got.Name)
	}
}
