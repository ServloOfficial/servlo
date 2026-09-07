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

# servlo env is what fills in host, port, user and password for whichever
# connection the site is on. Running it here proves that wiring rather than
# hand-writing credentials the panel would have written differently.
servlo env
echo "── .env after servlo env ──"
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
  echo "── fpm ──"; servlo logs --lines 60 2>&1 | tail -60 || true
  exit 1
fi

echo "$body"
echo "nginx to the site's own pool to PDO to MySQL, over HTTP, end to end"
