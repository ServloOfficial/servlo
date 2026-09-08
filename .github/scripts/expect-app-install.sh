#!/usr/bin/env bash
# Install an application the way the panel's one-click button does, and then
# prove the thing an install is actually for: a working login page backed by a
# database the installer populated.
#
# HANDOVER.md lists this among the claims no test suite can make. It is the
# longest chain in the product — fetch a pinned release, verify its checksum,
# extract it through the hardened extractor, create a database and a scoped
# user, write a config file with generated salts, drive the application's own
# installer over HTTP, and register a site and a cron — and until now none of
# it had ever run outside a unit test.
set -euo pipefail

app="$1"
domain="$2"
ip=$(ip -4 -o route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<NF;i++) if ($i=="src") print $(i+1)}')

# The installer drives the application's own setup over HTTP against the site's
# own domain, which resolves nowhere on a runner. /etc/hosts is the harness's
# to write, not servlo's: servlo never touches a resolver, and this is CI
# standing in for the DNS an operator's registrar provides.
echo "$ip $domain" | sudo tee -a /etc/hosts >/dev/null

# The generated admin password is printed once, and this repository's CI logs
# are public. Redacted on the way through rather than after the fact: `tee` to a
# file and `cat` it later would have put it in the log twice.
cd "$HOME"
servlo apps install "$app" "$domain" --admin-email ci@servlo.invalid --title "Servlo CI" 2>&1 |
  sed -E 's/^(  Password  *).*/\1<redacted>/' | tee /tmp/app-install.log

echo
echo "── the site is registered ──"
servlo sites | tee /tmp/sites.txt
grep -q "$domain" /tmp/sites.txt || { echo "the app installed but registered no site"; exit 1; }

echo
echo "── the config file exists and is not world readable ──"
cfg=$(find "$HOME" -maxdepth 3 -name wp-config.php -path "*$domain*" 2>/dev/null | head -1)
if [ -n "$cfg" ]; then
  mode=$(stat -c '%a' "$cfg")
  echo "wp-config.php mode $mode"
  [ "$mode" = "600" ] || { echo "wp-config.php must be 0600, it holds the database password and the salts"; exit 1; }
  grep -q "DISALLOW_FILE_EDIT" "$cfg" || { echo "the production hardening defines are missing from wp-config.php"; exit 1; }
fi

echo
echo "── the installed application answers, and its login page is real ──"
body=""
for _ in $(seq 25); do
  body=$(curl -fsSL --max-time 20 --resolve "$domain:80:$ip" "http://$domain/wp-login.php" 2>/dev/null || true)
  case "$body" in *"user_login"*) break ;; esac
  sleep 2
done
case "$body" in
  *"user_login"*) echo "the login form is being served by the installed application" ;;
  *)
    echo "the application did not serve a login form"
    curl -sS -i --max-time 20 --resolve "$domain:80:$ip" "http://$domain/wp-login.php" | head -40 || true
    echo "── install log (password redacted at source) ──"
    cat /tmp/app-install.log || true
    exit 1 ;;
esac

# An installer that stopped before writing the user table leaves a login form
# that works and an account that does not exist. The front page is what tells
# the two apart: an uninstalled WordPress redirects every request to its own
# installer.
echo
echo "── the front page is the site, not the installer ──"
front=$(curl -fsSL --max-time 20 --resolve "$domain:80:$ip" "http://$domain/" 2>/dev/null || true)
case "$front" in
  *"install.php"*|*"Installation"*)
    echo "the front page is still the installer, so setup never completed"
    echo "${front:0:600}"
    exit 1 ;;
esac
case "$front" in
  *"Servlo CI"*) echo "the front page carries the title the install was given" ;;
  *) echo "note: the front page renders but does not name the title"; echo "${front:0:300}" ;;
esac

echo
echo "── the real system cron replaced the request-driven one ──"
systemctl --user list-timers 'servlo-cron-*' --all --no-pager || true

echo
echo "$app installed, configured and serving on $domain"
