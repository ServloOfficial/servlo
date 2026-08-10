// Package origin centralises every URL servlo fetches its own assets from:
// release binaries, the framework, service and app stores, the changelog, and
// the GHCR base images. Each endpoint is overridable via its environment
// variable for tests and mirrors.
//
// Every one of them derives from mainRepo, so moving the project to an
// organisation is a one-line change here and nothing else in the tree.
package origin

import (
	"os"
	"strings"
)

// The repository is private today, so these endpoints 404 and every caller
// falls back: the stores to the copy embedded in the binary, the tools manifest
// to its embedded copy, the changelog to printing the release URL, the base
// images to a local build. That is acceptable because Servlo has published no
// releases yet, and it is still the right target — resolving Servlo's updates
// against the upstream release feed would hand a different project's binaries
// to a Servlo install.
const mainRepo = "realrashid/servlo" // releases, installer, stores, tools manifest, changelog, images

// imageOwner is the GHCR namespace the prebuilt PHP-FPM base images are
// published under, taken from mainRepo so the images follow the project rather
// than needing a second edit when it moves to an organisation.
func imageOwner() string {
	owner, _, _ := strings.Cut(mainRepo, "/")
	return owner
}

// storeBase is where a store's definitions are fetched from: this repository,
// under stores/. A private repository answers 404 there, which is why every
// binary also embeds the stores (see package stores) and the client falls
// through to that copy. The fetch is how a definition published since a build
// reaches an existing install; it is not how an install bootstraps.
func storeBase(kind string) string {
	return "https://raw.githubusercontent.com/" + mainRepo + "/main/stores/" + kind
}

// StoreBaseURLs returns the framework-store base: index.json plus
// <name>/<version>.yaml beneath it.
func StoreBaseURLs() []string {
	if list := splitList(os.Getenv("SERVLO_STORE_BASE_URL")); len(list) > 0 {
		return list
	}
	return []string{storeBase("frameworks")}
}

// ServiceStoreBaseURLs returns the service-preset-store base: index.json plus
// <name>.yaml beneath it.
func ServiceStoreBaseURLs() []string {
	if list := splitList(os.Getenv("SERVLO_SERVICES_BASE_URL")); len(list) > 0 {
		return list
	}
	return []string{storeBase("services")}
}

// AppStoreBaseURLs returns the one-click-app-store base: index.json plus
// <name>.yaml beneath it.
func AppStoreBaseURLs() []string {
	if list := splitList(os.Getenv("SERVLO_APPS_BASE_URL")); len(list) > 0 {
		return list
	}
	return []string{storeBase("apps")}
}

// ReleaseBaseURLs lists GitHub releases bases.
func ReleaseBaseURLs() []string {
	if list := splitList(os.Getenv("SERVLO_RELEASES_URL")); len(list) > 0 {
		return list
	}
	return []string{"https://github.com/" + mainRepo + "/releases"}
}

// ReleaseDownloadBases lists release-asset download bases.
func ReleaseDownloadBases() []string {
	if list := splitList(os.Getenv("SERVLO_RELEASE_DOWNLOAD_URL")); len(list) > 0 {
		return list
	}
	out := ReleaseBaseURLs()
	for i := range out {
		out[i] += "/download"
	}
	return out
}

// ReleaseAPIBaseURLs lists GitHub API bases.
func ReleaseAPIBaseURLs() []string {
	if list := splitList(os.Getenv("SERVLO_RELEASES_API_URL")); len(list) > 0 {
		return list
	}
	return []string{"https://api.github.com/repos/" + mainRepo}
}

// ToolsManifestURLs lists raw URLs of the pinned host-tool manifest
// (internal/tools/tools.yaml); the embedded copy is the fallback when none
// answer.
func ToolsManifestURLs() []string {
	if list := splitList(os.Getenv("SERVLO_TOOLS_URL")); len(list) > 0 {
		return list
	}
	return []string{"https://raw.githubusercontent.com/" + mainRepo + "/main/internal/tools/tools.yaml"}
}

// ExtraToolHosts lists additional hosts a published tool manifest may point at,
// for a test rig or a mirror. Empty by default: the built-in allowlist is what a
// normal install trusts.
func ExtraToolHosts() []string { return splitList(os.Getenv("SERVLO_TOOLS_HOSTS")) }

// ChangelogURLs lists raw changelog URLs.
func ChangelogURLs() []string {
	if list := splitList(os.Getenv("SERVLO_CHANGELOG_URL")); len(list) > 0 {
		return list
	}
	return []string{"https://raw.githubusercontent.com/" + mainRepo + "/main/CHANGELOG.md"}
}

// BaseImageRefs lists GHCR refs for a prebuilt PHP-FPM base image, where
// phpShort is the dotless version (e.g. "85") and hash pins the image to the
// embedded Containerfile template.
//
// A miss here is not a failure. The image is a shortcut past compiling every
// extension, and the caller falls back to building from the official
// php:<version>-fpm-alpine the Containerfile starts from, which is where the
// image came from in the first place. A slice rather than one ref so a
// namespace move can serve the old location as a fallback while binaries built
// before it are still in the field.
func BaseImageRefs(phpShort, hash string) []string {
	suffix := "/servlo-php" + phpShort + "-fpm-base:" + hash
	if v := os.Getenv("SERVLO_BASE_IMAGE_REGISTRY"); v != "" {
		return []string{v + suffix}
	}
	return []string{"ghcr.io/" + imageOwner() + suffix}
}

// splitList parses a comma-separated override into trimmed, non-empty entries.
func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
