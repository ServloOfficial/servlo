package podman

import (
	"strings"
	"testing"
)

// The shared FPM image is built on this machine and lives in local storage. An
// unqualified name in the unit sends podman to the registries in
// registries.conf to look for it, and a registry that answers with an error
// rather than a not-found is then a PHP-FPM that will not start: on a runner
// holding the image, a Docker Hub 500 produced
//
//	initializing source docker://servlo-php85-fpm:local: Requesting bearer
//	token: invalid status code from registry 500
//
// and every site on that machine was down. A droplet with restricted egress
// has the same shape permanently. internal/podman already qualifies the
// FrankenPHP image for this reason; the shared one was the odd case out.
func TestFPMQuadlet_NamesTheLocalImageWithoutAskingARegistry(t *testing.T) {
	content, err := renderFPMQuadletContent("8.5")
	if err != nil {
		t.Fatalf("renderFPMQuadletContent: %v", err)
	}
	if !strings.Contains(content, "Image=localhost/servlo-php85-fpm:local") {
		t.Errorf("the FPM quadlet must name the image in local storage explicitly:\n%s", content)
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "Image=") && !strings.HasPrefix(line, "Image=localhost/") {
			t.Errorf("unqualified image reference %q sends podman to a registry for an image servlo built", line)
		}
	}
}
