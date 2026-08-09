#!/usr/bin/env bash
# Create a site the way an operator would: a directory with an application in
# it, linked to a domain.
#
# The application is one PHP file that prints the domain back. Small on purpose:
# these jobs are testing that servlo brings a site back, not that a framework
# boots, and a framework install would add minutes and a second thing that can
# fail.
set -euo pipefail

# Resolved before anything changes directory, because $0 is the relative path
# the workflow invoked this with and stops resolving the moment we cd.
scripts="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

domain="$1"
root="$HOME/sites/$domain"

mkdir -p "$root/public"
cat > "$root/public/index.php" <<PHP
<?php
// The domain is echoed so a check can prove it reached this site rather than
// whichever vhost nginx serves by default.
echo "servlo-ci:$domain\n";
PHP

# A real database with a row in it, so the rebuild job proves data comes back
# rather than only that a site serves. A backup of a site with no database
# proves the smaller half of the story.
cat > "$root/.env" <<ENV
APP_ENV=production
DB_DATABASE=$(echo "$domain" | tr '.-' '__')
ENV

cd "$root"
servlo link "$domain"
servlo start

# The engine has to be answering before a database can be created in it, and
# starting servlo does not wait for that.
"$scripts/wait-for-db.sh"
servlo db:create

cat > /tmp/seed-$domain.sql <<SQL
CREATE TABLE IF NOT EXISTS servlo_ci (note VARCHAR(64));
INSERT INTO servlo_ci (note) VALUES ('$domain survived');
SQL
servlo db:import "/tmp/seed-$domain.sql"

servlo sites
