#!/usr/bin/env bash
# Assert a site actually answers, over HTTP, through nginx and its own pool.
#
# --resolve rather than a hosts entry: servlo never writes to a host resolver
# (CLAUDE.md §3.1) and its CI does not either. The Host header is what picks the
# vhost, and the body is checked as well as the status, because nginx's default
# vhost will happily answer 200 for a site that is not there.
#
# The check runs twice, against loopback and against the machine's own address,
# and the second one is the one that matters. A loopback-bound nginx answers
# 127.0.0.1 perfectly while serving nobody on the internet, which is exactly
# how a default-off exposure toggle survived here unnoticed.
set -euo pipefail

domain="$1"
want="servlo-ci:$domain"

serves() { # serves <addr> -> body on stdout
  curl -fsS --max-time 10 --resolve "$domain:80:$1" "http://$domain/" 2>/dev/null || true
}

report() { # report <addr>
  echo "last body: ${body:-<nothing>}"
  curl -sS -i --max-time 10 --resolve "$domain:80:$1" "http://$domain/" || true
  systemctl --user list-units 'servlo-*' --all --no-pager || true
  podman ps -a || true
}

# A pool that has just started takes a moment to accept its first connection,
# and a fixed sleep is either too short on a loaded runner or wasted time on a
# quiet one.
body=""
for attempt in $(seq 30); do
  body=$(serves 127.0.0.1)
  if [ "$body" = "$want" ]; then
    break
  fi
  sleep 2
done

if [ "$body" != "$want" ]; then
  echo "$domain never served its own page on loopback"
  report 127.0.0.1
  exit 1
fi

echo "$domain is serving on loopback"

host_ip=$(ip -4 -o route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<NF;i++) if ($i=="src") print $(i+1)}')
if [ -z "$host_ip" ]; then
  echo "no non-loopback address on this runner, cannot check the public bind"
  exit 0
fi

body=$(serves "$host_ip")
if [ "$body" != "$want" ]; then
  echo "$domain answers 127.0.0.1 but not $host_ip, so nginx is bound to loopback"
  report "$host_ip"
  exit 1
fi

echo "$domain is serving on $host_ip"
