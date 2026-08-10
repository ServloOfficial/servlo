#!/usr/bin/env bash
# Every unit servlo installed has to be enabled, or it does not come back at
# boot.
#
# This is the check that catches the failure before it happens. A unit that is
# running but not enabled looks perfectly healthy in every dashboard servlo has,
# and is gone the first time the droplet restarts, which is months later and
# during an upgrade nobody connects it to.
set -euo pipefail

dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
if [ ! -d "$dir" ]; then
  echo "servlo installed no units at all"
  exit 1
fi

failed=0
found=0
for path in "$dir"/servlo-*.service "$dir"/servlo-*.timer; do
  [ -e "$path" ] || continue
  unit=$(basename "$path")

  # A unit with no [Install] section is started by something else naming it,
  # which is how the backup and rotation timers start their own oneshots. Those
  # are correctly not enabled.
  if ! grep -q '^\[Install\]' "$path"; then
    echo "skip     $unit (nothing to enable: started by its timer)"
    continue
  fi
  found=$((found + 1))
  state=$(systemctl --user is-enabled "$unit" 2>/dev/null || true)
  if [ "$state" = "enabled" ] || [ "$state" = "enabled-runtime" ]; then
    echo "enabled  $unit"
  else
    echo "NOT ENABLED  $unit ($state)"
    failed=$((failed + 1))
  fi
done

if [ "$found" -eq 0 ]; then
  echo "no installable units were found, so this check proved nothing"
  exit 1
fi
if [ "$failed" -gt 0 ]; then
  echo "$failed unit(s) would not come back after a reboot"
  exit 1
fi
echo "all $found unit(s) come back at boot"
