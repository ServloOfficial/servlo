#!/usr/bin/env bash
# Take everything down, then bring it up the way a boot does.
#
# Starting default.target is what the user manager does when a lingering user's
# session begins: it pulls in every unit that is WantedBy=default.target and
# nothing else. So a unit servlo wrote but never enabled stays down here, which
# is the bug worth catching, and nothing in this script names a servlo unit to
# start. That matters: a script that started them by name would pass whether or
# not they were enabled, and would be testing itself.
set -euo pipefail

units=$(systemctl --user list-units 'servlo-*' --all --plain --no-legend --no-pager | awk '{print $1}')
if [ -z "$units" ]; then
  echo "no servlo units are installed, so there is nothing to bring back"
  exit 1
fi
echo "stopping:"
echo "$units"

# shellcheck disable=SC2086
systemctl --user stop $units || true
podman stop --all --time 10 || true

# Assert it really is down, or the check afterwards proves nothing.
for attempt in $(seq 15); do
  if ! curl -fsS --max-time 3 --resolve "${SITE:-ci-one.example}:80:127.0.0.1" \
      "http://${SITE:-ci-one.example}/" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if curl -fsS --max-time 3 --resolve "${SITE:-ci-one.example}:80:127.0.0.1" \
    "http://${SITE:-ci-one.example}/" >/dev/null 2>&1; then
  echo "the site is still serving after everything was stopped, so the check that follows would prove nothing"
  exit 1
fi

echo "starting default.target"
systemctl --user start default.target
