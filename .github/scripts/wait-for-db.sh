#!/usr/bin/env bash
# Wait until the database engine is answering queries, not merely running.
#
# A container that is up is not a server that is ready: MySQL initialises its
# data directory on first boot and refuses connections while it does, and the
# unit is active the whole time. So a script that creates a database straight
# after starting servlo races the engine, and loses on a slow runner with a
# message about a socket that says nothing about why.
#
# If it never answers, this prints what the engine itself said. A readiness
# wait that fails silently just moves the confusing error one step later.
set -euo pipefail

container="servlo-${1:-mysql}"

for attempt in $(seq 1 90); do
  # -h 127.0.0.1 for the same reason the preset's own commands carry it: the
  # client's default socket path is not where this image's server puts one, so
  # a socket connection fails against a server that is perfectly ready.
  if podman exec "$container" sh -c 'mysqladmin -h 127.0.0.1 ping 2>&1 | grep -q "is alive"'; then
    echo "$container is answering after ${attempt}s"
    exit 0
  fi
  sleep 1
done

echo "$container never answered in 90s"
podman ps -a || true
podman logs --tail 100 "$container" || true
podman exec "$container" sh -c 'mysqladmin -h 127.0.0.1 ping' || true
exit 1
