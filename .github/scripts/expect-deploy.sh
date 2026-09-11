#!/usr/bin/env bash
# Deploy a site the way the panel does, and check the three things a deploy
# promises that nothing outside a unit test had ever checked.
#
# Every external command in the deploy tests is stubbed, so what they prove is
# that the Go code makes the right decisions. They cannot prove a real pull, a
# real dump, or the thing that only exists because the container is real: a
# failed script withholds the PHP-FPM reload, and production OPcache is not
# watching timestamps, so visitors keep getting the bytecode that worked even
# though the new files are already on disk.
#
# Driven over the panel's own API rather than a helper binary, because the route,
# its permission and its audit entry are part of what a deploy is. Claiming the
# panel over loopback is how the first account is made without a TTY, and it
# exercises the one route that exists before anybody can be signed in.
set -euo pipefail

domain="$1"
panel="https://127.0.0.1:7073"
ip=$(ip -4 -o route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<NF;i++) if ($i=="src") print $(i+1)}')
jar=$(mktemp)
origin="$HOME/origin.git"
work="$HOME/work"
site="$HOME/sites/$domain"

# The password is generated here and never printed. This repository's CI logs
# are public, and an admin account on a panel is an admin account.
admin_pw=$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 20)

say() { echo; echo "── $* ──"; }

# ── a repository with something in it ───────────────────────────────────────
say "a repository the site can pull from"
git init -q --bare "$origin"
git init -q "$work"
cd "$work"
git config user.email ci@servlo.invalid
git config user.name "Servlo CI"
mkdir -p public
echo '<?php echo "servlo-ci:v1\n";' > public/index.php
# An artisan file, because the migration check turns on the framework's own
# declaration of what a migration looks like, and a directory servlo cannot
# place has no such declaration. Nothing runs it: the deploy script here is
# written by this job, not by the framework's template.
echo '#!/usr/bin/env php' > artisan
cat > .env <<ENV
APP_ENV=production
DB_CONNECTION=mysql
DB_HOST=servlo-mysql
DB_DATABASE=ci_deploy
ENV
git add public artisan .env
git commit -qm "the version that works"
git branch -M main
git remote add origin "$origin"
git push -q origin main

# ── the site, cloned from it ────────────────────────────────────────────────
say "the site, cloned from that repository"
mkdir -p "$(dirname "$site")"
git clone -q "$origin" "$site"
cd "$site"
git config user.email ci@servlo.invalid
git config user.name "Servlo CI"
servlo link "$domain"
servlo start
"$(dirname "${BASH_SOURCE[0]}")/wait-for-db.sh"
servlo db:create

# Production mode is what makes the failed-deploy check mean anything: OPcache
# stops watching timestamps, so the only thing that can put new code in front of
# a visitor is the reload a failed deploy withholds.
servlo production on

name=$(servlo sites | awk -v d="$domain" '$2 == d {print $1}')
[ -n "$name" ] || { echo "the site did not register"; servlo sites; exit 1; }
echo "site handle: $name"

serves() { curl -fsS --max-time 15 --resolve "$domain:80:$ip" "http://$domain/" 2>/dev/null || true; }

for _ in $(seq 30); do
  case "$(serves)" in *servlo-ci:v1*) break ;; esac
  sleep 2
done
case "$(serves)" in
  *servlo-ci:v1*) echo "the site serves its first version" ;;
  *) echo "the site never served its first version"; serves; servlo sites; exit 1 ;;
esac

# ── claim the panel, over loopback ──────────────────────────────────────────
say "claim the panel"
setup=$(curl -sk -c "$jar" --max-time 20 -X POST "$panel/api/auth/setup" \
  -H 'Content-Type: application/json' \
  -d "$(printf '{"username":"ci","password":"%s"}' "$admin_pw")")
csrf=$(echo "$setup" | sed -n 's/.*"csrf":"\([^"]*\)".*/\1/p')
[ -n "$csrf" ] || { echo "could not claim the panel: ${setup//$admin_pw/<redacted>}"; exit 1; }
echo "the panel has an admin and a session"

deploy() { # deploy -> the stream, so a caller can read the final event
  curl -sk -b "$jar" --max-time 300 -X POST "$panel/api/sites/$domain/deploy" \
    -H "X-Servlo-CSRF: $csrf"
}

script="$HOME/.local/share/servlo/deploy-scripts/$name.sh"
mkdir -p "$(dirname "$script")"

# ── a deploy pulls, runs the script, and puts the new code in front ─────────
say "a deploy makes the new commit live"
cd "$work"
echo '<?php echo "servlo-ci:v2\n";' > public/index.php
git commit -qam "the version being deployed"
git push -q origin main

cat > "$script" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
echo "the deploy script ran" > /tmp/deploy-ran
SH
chmod +x "$script"

out=$(deploy)
echo "$out" | tail -5
case "$out" in *'"ok":true'*) echo "the deploy reported success" ;;
  *) echo "the deploy did not report success"; echo "$out" | tail -30; exit 1 ;;
esac
[ -f /tmp/deploy-ran ] || { echo "the deploy script never ran"; exit 1; }

for _ in $(seq 15); do
  case "$(serves)" in *servlo-ci:v2*) break ;; esac
  sleep 2
done
case "$(serves)" in
  *servlo-ci:v2*) echo "the site serves the new commit" ;;
  *) echo "the pull and reload did not put the new code in front of a visitor"; serves; exit 1 ;;
esac

# ── a migrating script takes a snapshot first ───────────────────────────────
say "a script that migrates is backed up first"
before=$(servlo db:snapshots 2>/dev/null | grep -c predeploy || true)
cat > "$script" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
# The marker the framework declares for a migration. Nothing is actually
# migrated: what is being checked is that servlo took the snapshot before
# deciding to run this at all.
echo "php artisan migrate --force"
SH
chmod +x "$script"

cd "$work"
echo '<?php echo "servlo-ci:v3\n";' > public/index.php
git commit -qam "a release with a migration in its deploy"
git push -q origin main

out=$(deploy)
case "$out" in *'"ok":true'*) : ;; *) echo "the migrating deploy failed"; echo "$out" | tail -30; exit 1 ;; esac
servlo db:snapshots
after=$(servlo db:snapshots 2>/dev/null | grep -c predeploy || true)
[ "$after" -gt "$before" ] || { echo "no snapshot was taken before a deploy whose script migrates"; exit 1; }
echo "a snapshot was taken before the migration ran"

# ── a failing script leaves visitors on the version that worked ─────────────
say "a failing script does not put half a deploy in front of anyone"
cat > "$script" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
echo "about to fail, on purpose"
exit 1
SH
chmod +x "$script"

cd "$work"
echo '<?php echo "servlo-ci:v4-never-served\n";' > public/index.php
git commit -qam "a release whose deploy script fails"
git push -q origin main

out=$(deploy)
case "$out" in
  *'"ok":false'*) echo "the deploy reported the failure" ;;
  *) echo "a failing script was reported as a successful deploy"; echo "$out" | tail -30; exit 1 ;;
esac

# The files are on disk now: the pull succeeded and only the script failed. What
# a visitor gets is decided by whether servlo reloaded PHP-FPM, and it must not
# have.
grep -q "v4-never-served" "$site/public/index.php" || { echo "the pull did not happen, so this proves nothing"; exit 1; }
body=$(serves)
case "$body" in
  *servlo-ci:v4-never-served*)
    echo "a failed deploy put the half-deployed version in front of a visitor"
    exit 1 ;;
  *servlo-ci:v3*) echo "visitors are still on the version that worked" ;;
  *) echo "the site is serving something unexpected: $body"; exit 1 ;;
esac

echo
echo "deploy: pulled, ran, reloaded; backed up before a migration; and held the line on a failure"
