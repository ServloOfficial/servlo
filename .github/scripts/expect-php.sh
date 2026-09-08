#!/usr/bin/env bash
# Assert the PHP-FPM image servlo builds is the one it promises: the version it
# says, the extensions the Containerfile compiles, the production ini defaults
# CLAUDE.md §3.4 requires on every site, and the tooling an operator reaches for.
#
# Every check goes through `servlo php` / `servlo composer` — the host shims a
# person actually types — rather than `podman exec`. A shim that does not reach
# the container is exactly the kind of break this is here to catch, and calling
# podman directly would step over it.
set -euo pipefail

# Failures are collected and reprinted last. A CI log is read from the end, and
# the first cut of this printed them inline and then dumped the module list, so
# the only thing retrievable afterwards was the module list.
fail=0
failures=""
check() { # check <label> <expected-substring> <actual>
  if [[ "$3" == *"$2"* ]]; then
    printf '  ok   %s\n' "$1"
  else
    printf '  FAIL %s\n' "$1"
    failures+="  $1"$'\n'"       wanted: $2"$'\n'"       got:    ${3:0:200}"$'\n'
    fail=1
  fi
}

echo "── the binary and the image agree on a version ──"
want_version="$(servlo php:list 2>/dev/null | grep -oE '8\.[0-9]+' | head -1)"
[ -n "$want_version" ] || { echo "servlo php:list named no PHP version"; servlo php:list; exit 1; }
echo "servlo php:list reports $want_version"
version_out="$(servlo php -v 2>&1)"
check "php -v runs and reports $want_version" "PHP $want_version" "$version_out"

echo
echo "── the extensions the Containerfile compiles are loaded ──"
modules="$(servlo php -m 2>&1)"
# The set a PHP application on this panel actually depends on: database drivers,
# the string/image/intl trio a CMS will not boot without, the cache, and the
# session/queue backend. Not the full list — these are the ones whose absence is
# a broken site rather than a missing nicety.
for ext in pdo_mysql pdo_pgsql mysqli mbstring intl gd zip bcmath curl xml \
           opcache exif sockets pcntl soap xsl redis; do
  check "ext $ext" "$ext" "$modules"
done

echo
echo "── production ini defaults (CLAUDE.md §3.4) ──"
ini() { servlo php -r "echo ini_get('$1');" 2>/dev/null; }
check "display_errors is off"          ""    "$(ini display_errors)"
check "expose_php is off"              ""    "$(ini expose_php)"
check "opcache is enabled"             "1"   "$(ini opcache.enable)"
check "opcache does not stat on every request" "0" "$(ini opcache.validate_timestamps)"

echo
echo "── the tooling an operator reaches for ──"
check "composer runs in the container" "Composer version" "$(servlo composer --version 2>&1)"
check "node is in the image"           "v"                "$(servlo php -r 'echo shell_exec("node --version");' 2>&1)"
check "git is in the image"            "git version"      "$(servlo php -r 'echo shell_exec("git --version");' 2>&1)"

if [ "$fail" -ne 0 ]; then
  echo
  echo "════ what failed ════"
  printf '%s' "$failures"
  echo "════════════════════"
  exit 1
fi
echo
echo "the PHP image is what it claims to be"
