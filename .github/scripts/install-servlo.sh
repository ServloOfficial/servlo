#!/usr/bin/env bash
# Build this checkout and install it the way a server would have it: the binary
# at ~/.local/bin/servlo, and the one-time setup run unattended.
#
# Built rather than downloaded, because the point of these jobs is the code in
# the pull request, not the last release.
set -euo pipefail

make build-ui
go build -o "$HOME/.local/bin/servlo" ./cmd/servlo
servlo --version

# --unattended skips the sudo-gated system steps, which prepare-runtime.sh has
# already done. A database is included because a rebuild that restored files
# and no data would pass a test worth nothing.
servlo install --unattended --database mysql

# Said out loud rather than assumed. Autostart off is a legitimate choice and it
# means the quadlets lose their [Install] section, so the reboot job would fail
# for a reason that is not a bug. A server meant to come back on its own has
# this on, and the job is about that server.
servlo autostart enable

servlo status || true
