#!/usr/bin/env bash
# The whole serving stack in one request: nginx picks the vhost, hands it to the
# site's own PHP-FPM pool, PHP opens PDO over the podman network to the MySQL
# container, and the row make-site.sh seeded comes back out through HTTP.
#
# Every earlier job proves a site serves a static string. That says nothing
# about whether PHP can reach anything, which is the part an operator finds out
# about when their application shows a database error on a fresh install.
set -euo pipefail

domain="$1"
root="$HOME/sites/$domain"
cd "$root"

# Deliberately not `servlo env`. That command maps services into a project's
# .env using the framework store's env.services, so it needs a framework, and
# it refuses a plain PHP application by design. This fixture is one file that
# echoes a row: giving it a framework to satisfy a helper would be testing the
# helper, and what is under test here is whether PHP can reach the database at
# all.
#
# So the connection comes from where servlo itself keeps it. The service
# password is one file, generated at install; the host is the engine's container
# on the podman network.
password="$(cat "$HOME/.config/servlo/service-password")"
{
  echo "DB_HOST=servlo-mysql"
  echo "DB_PORT=3306"
  echo "DB_USERNAME=root"
  echo "DB_PASSWORD=$password"
} >> .env
echo "── .env (password redacted) ──"
sed -E 's/(PASSWORD=).*/\1<redacted>/' .env

cat > public/index.php <<'PHP'
<?php
// Read the connection the panel wrote, the way a framework's config would.
$env = [];
foreach (file(__DIR__ . '/../.env', FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES) as $line) {
    if ($line === '' || $line[0] === '#' || !str_contains($line, '=')) continue;
    [$k, $v] = explode('=', $line, 2);
    $env[trim($k)] = trim($v, " \t\"'");
}

$dsn = sprintf('mysql:host=%s;port=%s;dbname=%s',
    $env['DB_HOST'] ?? '127.0.0.1', $env['DB_PORT'] ?? '3306', $env['DB_DATABASE'] ?? '');

try {
    $pdo = new PDO($dsn, $env['DB_USERNAME'] ?? '', $env['DB_PASSWORD'] ?? '', [
        PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION,
    ]);
    $note = $pdo->query('SELECT note FROM servlo_ci LIMIT 1')->fetchColumn();
    printf("servlo-db:%s\n", $note);
} catch (Throwable $e) {
    http_response_code(500);
    printf("servlo-db-error:%s\n", $e->getMessage());
}
PHP

# The image checks turned production mode on earlier in this job, which sets
# opcache.validate_timestamps=0 — PHP stops checking whether a file changed and
# keeps serving the bytecode it already has. The page just written over the
# fixture's own index.php is therefore invisible until the master re-reads.
#
# That is not a defect, it is what production mode is for, and servlo says so
# itself: `servlo production on` prints "apply it to the running stack with:
# servlo restart". Doing it here tests that instruction rather than working
# around it. The first cut of this script did not, and the site answered with
# the fixture's original string, which is exactly the failure a real operator
# gets when they edit a file on a production box and nothing changes.
servlo restart

ip=$(ip -4 -o route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<NF;i++) if ($i=="src") print $(i+1)}')
want="servlo-db:$domain survived"

body=""
for _ in $(seq 20); do
  body=$(curl -fsS --max-time 15 --resolve "$domain:80:$ip" "http://$domain/" 2>/dev/null || true)
  [ "$body" = "$want" ] && break
  sleep 2
done

if [ "$body" != "$want" ]; then
  echo "PHP did not read the seeded row back through HTTP"
  echo "wanted: $want"
  echo "got:    ${body:-<nothing>}"
  curl -sS -i --max-time 15 --resolve "$domain:80:$ip" "http://$domain/" || true
  echo "── fpm ──"; podman logs servlo-php85-fpm 2>&1 | tail -25 || true
  exit 1
fi

echo "$body"
echo "nginx to the site's own pool to PDO to MySQL, over HTTP, end to end"
