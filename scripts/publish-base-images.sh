#!/usr/bin/env bash
# Build and publish the prebuilt PHP-FPM base images by hand.
#
# This is what .github/workflows/base-images.yml would do if GitHub Actions
# were available. It is not a fallback for a broken workflow: it is the same
# recipe, driven from a machine you own, and either route produces images the
# client accepts because the tag is derived from the Containerfile rather than
# from who built it.
#
# You need: podman or docker, and a GitHub token with write:packages exported
# as GHCR_TOKEN. The images are public and MIT, same as everything else here.
#
#   export GHCR_TOKEN=ghp_...
#   scripts/publish-base-images.sh                 # every version, this arch
#   scripts/publish-base-images.sh 8.4 8.3         # just those two
#   OWNER=my-org scripts/publish-base-images.sh    # publish under an org
#
# Skipping this entirely is a supported choice. Without a published image an
# install builds from the official php:<version>-fpm-alpine and compiles the
# extensions on the machine, which is slower on first run and identical after.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

RECIPE="internal/podman/quadlets/servlo-php-fpm.Containerfile"
ALL_VERSIONS=(7.4 8.0 8.1 8.2 8.3 8.4 8.5)
# Lowercased: GHCR refuses a reference containing capitals, and an
# organisation name is free to have them.
OWNER="$(printf '%s' "${OWNER:-realrashid}" | tr '[:upper:]' '[:lower:]')"
ENGINE="${ENGINE:-$(command -v podman || command -v docker)}"

versions=("$@")
[ ${#versions[@]} -eq 0 ] && versions=("${ALL_VERSIONS[@]}")

if [ -z "$ENGINE" ]; then
  echo "need podman or docker on PATH" >&2
  exit 1
fi
if [ -z "${GHCR_TOKEN:-}" ]; then
  echo "set GHCR_TOKEN to a GitHub token with write:packages" >&2
  exit 1
fi

# The tag is the hash of the recipe with the user-customisation placeholders
# emptied, truncated to 12. It has to match baseContainerfileHash in
# internal/podman/build.go exactly, or the client asks for a tag that is not
# there and quietly builds from source instead. Same three placeholders, same
# order, same truncation.
tag() {
  python3 - "$RECIPE" <<'EOF'
import hashlib, sys
content = open(sys.argv[1]).read()
for ph in ('{{.CustomExtensions}}', '{{.CustomExtensionsRuntime}}', '{{.CustomPackages}}'):
    content = content.replace(ph, '')
print(hashlib.sha256(content.encode()).hexdigest()[:12])
EOF
}

TAG="$(tag)"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

echo "recipe tag : $TAG"
echo "owner      : $OWNER"
echo "arch       : $ARCH"
echo "engine     : $ENGINE"
echo "versions   : ${versions[*]}"
echo

echo "$GHCR_TOKEN" | "$ENGINE" login ghcr.io -u "$OWNER" --password-stdin

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

for v in "${versions[@]}"; do
  short="${v//./}"
  image="ghcr.io/${OWNER}/servlo-php${short}-fpm-base:${TAG}-${ARCH}"

  python3 - "$RECIPE" "$v" "$work/Containerfile" <<'EOF'
import sys
recipe, version, out = sys.argv[1], sys.argv[2], sys.argv[3]
content = open(recipe).read().replace('{{.Version}}', version)
for ph in ('{{.CustomExtensions}}', '{{.CustomExtensionsRuntime}}', '{{.CustomPackages}}'):
    content = content.replace(ph, '')
open(out, 'w').write(content)
EOF

  echo "==> PHP $v  ->  $image"
  "$ENGINE" build -f "$work/Containerfile" -t "$image" .

  # Same floor the workflow enforces: a recipe edit that silently drops
  # extensions should fail here rather than in somebody's rebuild.
  count="$("$ENGINE" run --rm "$image" php -m | grep -vc '^\[\|^$')"
  echo "    $count modules"
  if [ "$count" -lt 50 ]; then
    echo "::error:: PHP $v loaded only $count modules, expected at least 50" >&2
    exit 1
  fi
  "$ENGINE" run --rm "$image" composer --version >/dev/null

  "$ENGINE" push "$image"
done

echo
echo "Pushed ${#versions[@]} image(s) for $ARCH at tag $TAG."
echo
echo "The client pulls the un-suffixed tag, so each version needs a manifest"
echo "joining its architectures. Once both arches are pushed, run for each:"
echo
for v in "${versions[@]}"; do
  short="${v//./}"
  base="ghcr.io/${OWNER}/servlo-php${short}-fpm-base"
  echo "  $ENGINE manifest create $base:$TAG $base:$TAG-amd64 $base:$TAG-arm64 && $ENGINE manifest push $base:$TAG"
done
echo
echo "Building only one architecture is fine. Publish the manifest with the"
echo "single arch you have, and machines of the other kind build from source,"
echo "which is what they do today anyway."
