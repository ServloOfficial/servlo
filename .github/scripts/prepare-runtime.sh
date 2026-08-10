#!/usr/bin/env bash
# Make a GitHub runner look like the machine servlo expects: a lingering user
# with its own systemd instance, rootless podman, and ports 80 and 443 reachable
# without root.
#
# The sysctl is run here with sudo rather than by servlo. That division is the
# rule, not a CI shortcut: servlo prints the command that needs privilege and a
# person runs it (CLAUDE.md §3.2). In CI the workflow is the person.
set -euo pipefail

sudo loginctl enable-linger "$USER"
uid=$(id -u)
echo "XDG_RUNTIME_DIR=/run/user/$uid" >> "$GITHUB_ENV"
echo "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$uid/bus" >> "$GITHUB_ENV"
export XDG_RUNTIME_DIR="/run/user/$uid"
export DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$uid/bus"

# The user manager takes a moment to come up after linger is switched on.
# Without its bus, every systemctl --user call below fails in a way that reads
# like servlo is broken.
for _ in $(seq 30); do
  [ -S "/run/user/$uid/bus" ] && break
  sleep 1
done
if [ ! -S "/run/user/$uid/bus" ]; then
  echo "the user session bus never appeared at /run/user/$uid/bus"
  exit 1
fi

if ! command -v podman >/dev/null; then
  sudo apt-get update -qq
  sudo apt-get install -y -qq podman
fi
podman --version

# What a servlo install does on a droplet, which is the one thing on the machine
# that needs root: let an unprivileged process bind 80 and 443.
sudo sysctl -w net.ipv4.ip_unprivileged_port_start=0

# ~/.local/bin is where servlo installs itself and is not on a runner's PATH.
mkdir -p "$HOME/.local/bin"
echo "$HOME/.local/bin" >> "$GITHUB_PATH"
