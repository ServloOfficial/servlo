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
# Lower-cased, because php -m does not agree with itself on case: the extension
# built as "opcache" is listed as "Zend OPcache", and Core, PDO and SPL are all
# capitalised too. Matching the names as written would fail on the one extension
# whose absence matters most.
modules="$(servlo php -m 2>&1 | tr '[:upper:]' '[:lower:]')"
# The set a PHP application on this panel actually depends on: database drivers,
# the string/image/intl trio a CMS will not boot without, the cache, and the
# session/queue backend. Not the full list — these are the ones whose absence is
# a broken site rather than a missing nicety.
for ext in pdo_mysql pdo_pgsql mysqli mbstring intl gd zip bcmath curl xml \
           opcache exif sockets pcntl soap xsl redis; do
  check "ext $ext" "$ext" "$modules"
done

echo
echo "── the production switch actually switches something ──"
#
# The first cut of this asserted the production defaults on a fresh install and
# was simply wrong about the product. Production mode is off by default and
# says why in its own source: turning it on for somebody who has not asked hides
# the errors they were about to read. A fresh machine is supposed to show them.
#
# So the real claim, and the one an operator depends on the afternoon they go
# live, is that the single flag moves all of these together. CLAUDE.md §3.4 lists
# them as one decision precisely so a machine cannot end up half-production,
# showing stack traces to the world while caching hard enough to hide the fix.
ini() { servlo php -r "echo ini_get('$1');" 2>/dev/null; }

# check() matches a substring, and every string contains the empty one, so
# passing "" as the expected value asserts nothing at all. An off directive
# reads back as "", "0" or "Off" depending on how it was written, so it needs
# its own comparison.
is_off() { # is_off <value>
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    ""|0|off|false) return 0 ;;
    *) return 1 ;;
  esac
}
check_off() { # check_off <label> <actual>
  if is_off "$2"; then
    printf '  ok   %s\n' "$1"
  else
    printf '  FAIL %s\n' "$1"
    failures+="  $1"$'\n'"       wanted it off, got: $2"$'\n'
    fail=1
  fi
}
check_on() { # check_on <label> <actual>
  if is_off "$2"; then
    printf '  FAIL %s\n' "$1"
    failures+="  $1"$'\n'"       wanted it on, got: $2"$'\n'
    fail=1
  else
    printf '  ok   %s\n' "$1"
  fi
}

echo "  a fresh install is in development mode, and shows errors:"
check_on "display_errors is on before production mode" "$(ini display_errors)"

echo "  turning production mode on:"
servlo production on

check_off "display_errors is off in production"  "$(ini display_errors)"
check_off "expose_php is off in production"      "$(ini expose_php)"
check "opcache is enabled"                       "1" "$(ini opcache.enable)"
check "opcache stops stat-ing on every request"  "0" "$(ini opcache.validate_timestamps)"

echo
echo "── the tooling an operator reaches for ──"
check "composer runs in the container" "Composer version" "$(servlo composer --version 2>&1)"

# Asked for the binary's path rather than its version string. "v" as an expected
# substring, which is what this looked for first, appears in almost any output
# including the error text printed when the lookup fails, so it would have passed
# on an image with no node in it at all.
check "node is in the image" "/node" "$(servlo php -r 'echo shell_exec("command -v node");' 2>&1)"
check "npm is in the image"  "/npm"  "$(servlo php -r 'echo shell_exec("command -v npm");' 2>&1)"
check "git is in the image"  "/git"  "$(servlo php -r 'echo shell_exec("command -v git");' 2>&1)"

if [ "$fail" -ne 0 ]; then
  echo
  echo "════ what failed ════"
  printf '%s' "$failures"
  echo "════════════════════"
  exit 1
fi
echo
echo "the PHP image is what it claims to be"
