#!/usr/bin/env bash
# Assert a site actually answers, over HTTP, through nginx and its own pool.
#
# --resolve rather than a hosts entry: servlo never writes to a host resolver
# (CLAUDE.md §3.1) and its CI does not either. The Host header is what picks the
# vhost, and the body is checked as well as the status, because nginx's default
# vhost will happily answer 200 for a site that is not there.
set -euo pipefail

domain="$1"
want="servlo-ci:$domain"

# A pool that has just started takes a moment to accept its first connection,
# and a fixed sleep is either too short on a loaded runner or wasted time on a
# quiet one.
for attempt in $(seq 30); do
  body=$(curl -fsS --max-time 10 --resolve "$domain:80:127.0.0.1" "http://$domain/" 2>/dev/null || true)
  if [ "$body" = "$want" ]; then
    echo "$domain is serving"
    exit 0
  fi
  sleep 2
done

echo "$domain never served its own page"
echo "last body: ${body:-<nothing>}"
curl -sS -i --max-time 10 --resolve "$domain:80:127.0.0.1" "http://$domain/" || true
systemctl --user list-units 'servlo-*' --all --no-pager || true
podman ps -a || true
exit 1
