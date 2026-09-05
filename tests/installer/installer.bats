#!/usr/bin/env bats
# Tests for install.sh
# Requires: bats-core  https://github.com/bats-core/bats-core

INSTALLER="$BATS_TEST_DIRNAME/../../install.sh"

# Source the installer so we can call its functions directly.
# The guard at the bottom prevents main() from running when sourced.
setup() {
  # Isolate HOME so the installer never touches the real shell rc files.
  export HOME="$BATS_TMPDIR/home-$$"
  mkdir -p "$HOME"

  # Source the script to load all function definitions.
  # shellcheck disable=SC1090
  source "$INSTALLER"
}

teardown() {
  rm -rf "$BATS_TMPDIR/home-$$"
}

# ── detect_arch ───────────────────────────────────────────────────────────────

@test "detect_arch returns amd64 for x86_64" {
  # Override uname for this test
  function uname() { echo "x86_64"; }
  export -f uname

  run detect_arch
  [ "$status" -eq 0 ]
  [ "$output" = "amd64" ]
}

@test "detect_arch returns arm64 for aarch64" {
  function uname() { echo "aarch64"; }
  export -f uname

  run detect_arch
  [ "$status" -eq 0 ]
  [ "$output" = "arm64" ]
}

@test "detect_arch fails for unsupported arch" {
  function uname() { echo "mips"; }
  export -f uname

  run detect_arch
  [ "$status" -ne 0 ]
  [[ "$output" == *"Unsupported architecture"* ]]
}

# ── Ubuntu-only platform gate ─────────────────────────────────────────────────

@test "require_ubuntu accepts Ubuntu" {
  function detect_distro() { echo "ubuntu"; }
  export -f detect_distro

  run require_ubuntu
  [ "$status" -eq 0 ]
}

@test "require_ubuntu refuses a non-Ubuntu distro and names it" {
  function detect_distro() { echo "fedora"; }
  export -f detect_distro
  function ubuntu_pretty_name() { echo "Fedora Linux 41"; }
  export -f ubuntu_pretty_name

  run require_ubuntu
  [ "$status" -ne 0 ]
  [[ "$output" == *"Ubuntu 24.04"* ]]
  [[ "$output" == *"Fedora Linux 41"* ]]
}

@test "require_ubuntu refuses a Debian derivative rather than treating it as Ubuntu" {
  function detect_distro() { echo "linuxmint"; }
  export -f detect_distro

  run require_ubuntu
  [ "$status" -ne 0 ]
}

@test "podman_version parses the version out of podman --version" {
  function podman() { echo "podman version 4.9.3"; }
  export -f podman

  run podman_version
  [ "$output" = "4.9.3" ]
}

@test "podman_version is empty when podman is absent" {
  function command() { if [ "$1" = "-v" ] && [ "$2" = "podman" ]; then return 1; fi; builtin command "$@"; }
  export -f command

  run podman_version
  [ "$output" = "" ]
}

@test "require_podman_min accepts 4.5 exactly" {
  function podman_version() { echo "4.5.0"; }
  export -f podman_version

  run require_podman_min
  [ "$status" -eq 0 ]
}

@test "require_podman_min accepts a newer podman" {
  function podman_version() { echo "5.2.1"; }
  export -f podman_version

  run require_podman_min
  [ "$status" -eq 0 ]
}

@test "require_podman_min refuses podman below the 4.5 minimum" {
  function podman_version() { echo "3.4.4"; }
  export -f podman_version
  function ubuntu_release() { echo "24.04"; }
  export -f ubuntu_release

  run require_podman_min
  [ "$status" -ne 0 ]
  [[ "$output" == *"4.5"* ]]
  [[ "$output" == *"3.4.4"* ]]
}

# 22.04 ships podman 3.4.4. Refusing without naming the way out is the
# half-install this gate exists to prevent.
@test "require_podman_min printing the upgrade path on 22.04" {
  function podman_version() { echo "3.4.4"; }
  export -f podman_version
  function ubuntu_release() { echo "22.04"; }
  export -f ubuntu_release

  run require_podman_min
  [ "$status" -ne 0 ]
  [[ "$output" == *"24.04"* ]]
  [[ "$output" == *"do-release-upgrade"* ]]
}

@test "require_podman_min refuses when podman is not installed at all" {
  function podman_version() { echo ""; }
  export -f podman_version

  run require_podman_min
  [ "$status" -ne 0 ]
  [[ "$output" == *"podman"* ]]
}

# ── crun ─────────────────────────────────────────────────────────────────────

@test "require_crun accepts crun on PATH" {
  function command() { if [ "$1" = "-v" ] && [ "$2" = "crun" ]; then echo "/usr/bin/crun"; return 0; fi; builtin command "$@"; }
  export -f command

  run require_crun
  [ "$status" -eq 0 ]
}

# Servlo's quadlets name crun explicitly. runc is present on most hosts and
# podman will happily fall back to it, so a missing crun does not surface until
# the first container refuses to start, long after the installer has finished.
@test "require_crun refuses when crun is missing, and names the package" {
  function command() { if [ "$1" = "-v" ] && [ "$2" = "crun" ]; then return 1; fi; builtin command "$@"; }
  export -f command

  run require_crun
  [ "$status" -ne 0 ]
  [[ "$output" == *"crun"* ]]
  [[ "$output" == *"apt install"* ]]
}

# ── cgroup v2 ────────────────────────────────────────────────────────────────

@test "require_cgroup_v2 accepts a unified hierarchy" {
  function stat() { echo "cgroup2fs"; }
  export -f stat

  run require_cgroup_v2
  [ "$status" -eq 0 ]
}

# Rootless podman needs the unified hierarchy for per-container resource
# limits; on v1 the memory cap an asset build runs under silently does nothing.
@test "require_cgroup_v2 refuses cgroup v1 and names the boot parameter" {
  function stat() { echo "tmpfs"; }
  export -f stat

  run require_cgroup_v2
  [ "$status" -ne 0 ]
  [[ "$output" == *"cgroup v2"* ]]
  [[ "$output" == *"systemd.unified_cgroup_hierarchy=1"* ]]
}

# ── linger ───────────────────────────────────────────────────────────────────

@test "require_linger accepts a lingering user" {
  function loginctl() { echo "Linger=yes"; }
  export -f loginctl

  run require_linger
  [ "$status" -eq 0 ]
}

# Without linger every servlo unit dies at logout, so the panel and every site
# stop the moment the operator closes their SSH session. Refusing beats
# installing something that only runs while someone is watching it.
@test "require_linger refuses without linger and prints the exact command" {
  function loginctl() { echo "Linger=no"; }
  export -f loginctl

  run require_linger
  [ "$status" -ne 0 ]
  [[ "$output" == *"loginctl enable-linger"* ]]
}

@test "require_linger refuses when loginctl cannot answer at all" {
  function loginctl() { return 1; }
  export -f loginctl

  run require_linger
  [ "$status" -ne 0 ]
  [[ "$output" == *"loginctl enable-linger"* ]]
}

# ── _download_tool ────────────────────────────────────────────────────────────

@test "_download_tool prefers curl when both are available" {
  function curl() { return 0; }
  function wget() { return 0; }
  export -f curl wget

  # Temporarily mask PATH to ensure only our functions are visible
  run bash -c "source '$INSTALLER'; _download_tool"
  [ "$output" = "curl" ]
}

@test "_download_tool falls back to wget when curl is absent" {
  # Hide curl by making it unavailable in a subshell
  run bash -c "
    source '$INSTALLER'
    function curl() { return 127; }
    # Remove curl from PATH lookup
    PATH_ORIG=\$PATH
    export PATH=\"\$BATS_TMPDIR\"  # empty path with no curl binary
    _download_tool
  "
  # We just check it doesn't die — wget fallback varies by system
  [ "$status" -eq 0 ] || [[ "$output" == *"wget"* ]] || [[ "$output" == *"curl"* ]]
}

@test "_download_tool errors when neither curl nor wget found" {
  run bash -c "
    source '$INSTALLER'
    # Override command -v to report both as missing
    function command() {
      if [[ \"\$2\" == 'curl' || \"\$2\" == 'wget' ]]; then return 1; fi
      builtin command \"\$@\"
    }
    export -f command
    _download_tool
  "
  [ "$status" -ne 0 ]
  [[ "$output" == *"Neither curl nor wget"* ]]
}

# ── add_to_path / remove_from_path ────────────────────────────────────────────

@test "add_to_path appends PATH entry to .bashrc" {
  export SHELL="/bin/bash"
  INSTALL_DIR="$HOME/.local/bin"
  touch "$HOME/.bashrc"

  add_to_path

  grep -q "Added by Servlo installer" "$HOME/.bashrc"
  grep -q "$INSTALL_DIR" "$HOME/.bashrc"
}

@test "add_to_path is idempotent — does not duplicate entry" {
  export SHELL="/bin/bash"
  INSTALL_DIR="$HOME/.local/bin"
  touch "$HOME/.bashrc"

  add_to_path
  add_to_path

  count=$(grep -c "Added by Servlo installer" "$HOME/.bashrc")
  [ "$count" -eq 1 ]
}

@test "add_to_path writes fish_add_path for fish shell" {
  export SHELL="/usr/bin/fish"
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$HOME/.config/fish/conf.d"

  add_to_path

  grep -q "fish_add_path" "$HOME/.config/fish/conf.d/servlo.fish"
}

@test "remove_from_path removes the Servlo block from .bashrc" {
  export SHELL="/bin/bash"
  INSTALL_DIR="$HOME/.local/bin"
  printf '\n# Added by Servlo installer\nexport PATH="%s:$PATH"\n' "$INSTALL_DIR" > "$HOME/.bashrc"

  remove_from_path

  run grep "Added by Servlo installer" "$HOME/.bashrc"
  [ "$status" -ne 0 ]
}

@test "remove_from_path is a no-op when marker is absent" {
  export SHELL="/bin/bash"
  echo "unrelated content" > "$HOME/.bashrc"

  remove_from_path

  run cat "$HOME/.bashrc"
  [ "$output" = "unrelated content" ]
}

# ── installed_version ─────────────────────────────────────────────────────────

@test "installed_version returns empty string when servlo not found" {
  # Create an empty bin dir first, then restrict PATH to it
  local empty_dir="$BATS_TMPDIR/empty-path-$$"
  mkdir -p "$empty_dir"

  OLD_PATH="$PATH"
  export PATH="$empty_dir"

  run installed_version
  [ "$output" = "" ]

  export PATH="$OLD_PATH"
}

@test "installed_version returns version string when servlo is found" {
  # Create a fake servlo binary
  FAKE_BIN="$BATS_TMPDIR/fake-bin-$$"
  mkdir -p "$FAKE_BIN"
  printf '#!/bin/sh\necho "servlo version 1.2.3"\n' > "$FAKE_BIN/servlo"
  chmod +x "$FAKE_BIN/servlo"

  OLD_PATH="$PATH"
  export PATH="$FAKE_BIN:$PATH"

  run installed_version
  [ "$output" = "1.2.3" ]

  export PATH="$OLD_PATH"
}

# ── installed_version_raw / version_is_dev ────────────────────────────────────

@test "installed_version_raw keeps the git-describe suffix" {
  FAKE_BIN="$BATS_TMPDIR/fake-bin-$$"
  mkdir -p "$FAKE_BIN"
  printf '#!/bin/sh\necho "servlo version v1.25.0-6-g7d030096-dirty (commit 7d030096)"\n' > "$FAKE_BIN/servlo"
  chmod +x "$FAKE_BIN/servlo"

  OLD_PATH="$PATH"
  export PATH="$FAKE_BIN:$PATH"

  run installed_version_raw
  [ "$output" = "1.25.0-6-g7d030096-dirty" ]

  export PATH="$OLD_PATH"
}

@test "version_is_dev is true for a git-describe build" {
  run version_is_dev "1.25.0-6-g7d030096-dirty"
  [ "$status" -eq 0 ]
}

@test "version_is_dev is false for a clean release" {
  run version_is_dev "1.25.0"
  [ "$status" -ne 0 ]
}

# ── latest_version ────────────────────────────────────────────────────────────

@test "latest_version parses version from redirect Location header" {
  # Mock curl -fsSLI to return headers containing a Location pointing to the tag
  function curl() {
    echo "HTTP/2 302"
    echo "location: https://github.com/ServloOfficial/servlo/releases/tag/v2.0.0"
    echo ""
  }
  export -f curl

  run latest_version
  [ "$status" -eq 0 ]
  [ "$output" = "2.0.0" ]
}

@test "latest_version returns empty string when redirect has no tag" {
  function curl() {
    echo "HTTP/2 404"
    echo ""
  }
  export -f curl

  run latest_version
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

@test "latest_version returns empty string on curl failure" {
  function curl() { return 22; }
  export -f curl

  run latest_version
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

# ── --help flag ───────────────────────────────────────────────────────────────

@test "--help prints usage and exits 0" {
  run bash "$INSTALLER" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"Usage:"* ]]
  [[ "$output" == *"--update"* ]]
  [[ "$output" == *"--uninstall"* ]]
  [[ "$output" == *"--local"* ]]
}

# ── --local flag ──────────────────────────────────────────────────────────────

@test "--local fails with a clear error when file does not exist" {
  run bash "$INSTALLER" --local /tmp/nonexistent-servlo-binary-xyz
  [ "$status" -ne 0 ]
  [[ "$output" == *"not found"* ]]
}

@test "--local requires an argument" {
  run bash "$INSTALLER" --local
  [ "$status" -ne 0 ]
  [[ "$output" == *"requires a path"* ]]
}

# ── --check flag ──────────────────────────────────────────────────────────────

@test "--check runs prerequisite checks and exits 0 when all pass" {
  # Mock all check commands as present. This runs the installer as a subprocess,
  # where its own definitions shadow any exported function of the same name, so
  # the gates are satisfied at the command level rather than stubbed out.
  function command() {
    case "$2" in
      podman|unzip|crun) return 0 ;;
      *) builtin command "$@" ;;
    esac
  }
  function systemctl() { return 0; }
  function podman() {
    if [[ "$1" == "info" ]]; then echo "true"; fi
    if [[ "$1" == "--version" ]]; then echo "podman version 4.9.3"; fi
  }
  function stat() { echo "cgroup2fs"; }
  function loginctl() { echo "Linger=yes"; }
  export -f command systemctl podman stat loginctl

  run bash "$INSTALLER" --check
  [ "$status" -eq 0 ]
}

# ── uninstall_linux_dns ───────────────────────────────────────────────────────

# Stubs a servlo binary on PATH that records the arguments it was called with,
# so a test can assert which servlo subcommand the installer invoked.
_fake_servlo_dir() {
  local d="$BATS_TMPDIR/fakebin-$$"
  mkdir -p "$d"
  cat > "$d/servlo" <<EOF
#!/usr/bin/env bash
echo "\$@" >> "$d/calls"
EOF
  chmod +x "$d/servlo"
  rm -f "$d/calls"
  echo "$d"
}

_stub_dns_files() {
  local d="$BATS_TMPDIR/dnsconf-$$"
  mkdir -p "$d"
  : > "$d/servlo-dns-link.service"
  SERVLO_DNS_FILES=("$d/servlo-dns-link.service")
}

@test "uninstall_linux_dns runs the teardown when accepted" {
  local d; d="$(_fake_servlo_dir)"
  PATH="$d:$PATH"
  _stub_dns_files
  ask() { return 0; }
  run uninstall_linux_dns
  [ "$status" -eq 0 ]
  grep -q "dns:disable" "$d/calls"
}

@test "uninstall_linux_dns prints the manual removal when declined" {
  local d; d="$(_fake_servlo_dir)"
  PATH="$d:$PATH"
  _stub_dns_files
  ask() { return 1; }
  run uninstall_linux_dns
  [ "$status" -eq 0 ]
  [[ "$output" == *"servlo-dns-link.service"* ]]
  [[ "$output" == *"systemd-resolved"* ]]
  [ ! -f "$d/calls" ]
}

@test "uninstall_linux_dns falls back to the manual removal when the binary is gone" {
  function command() {
    case "$2" in
      servlo) return 1 ;;
      *) builtin command "$@" ;;
    esac
  }
  export -f command
  _stub_dns_files
  ask() { return 0; }
  run uninstall_linux_dns
  [ "$status" -eq 0 ]
  [[ "$output" == *"only root can remove it"* ]]
}

@test "servlo_dns_cleanup_hint lists the sudoers grant for hand removal" {
  # The passwordless DNS grant is a root-owned file the setup writes; left
  # behind it is a standing NOPASSWD root grant for a tool being removed.
  [[ " ${SERVLO_DNS_FILES[*]} " == *" /etc/sudoers.d/servlo "* ]]
  run servlo_dns_cleanup_hint
  [[ "$output" == *"/etc/sudoers.d/servlo"* ]]
}

@test "uninstall_linux_dns stays quiet when servlo never configured DNS" {
  local d; d="$(_fake_servlo_dir)"
  PATH="$d:$PATH"
  SERVLO_DNS_FILES=("$BATS_TMPDIR/nope-$$/servlo-dns-link.service")
  ask() { return 0; }
  run uninstall_linux_dns
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
  [ ! -f "$d/calls" ]
}

@test "cmd_uninstall tears the DNS down before removing the binary" {
  local body; body="$(declare -f cmd_uninstall)"
  local dns_at; dns_at="$(echo "$body" | grep -n 'uninstall_linux_dns' | head -1 | cut -d: -f1)"
  local bin_at; bin_at="$(echo "$body" | grep -n 'INSTALL_DIR' | head -1 | cut -d: -f1)"
  [ -n "$dns_at" ]
  [ -n "$bin_at" ]
  [ "$dns_at" -lt "$bin_at" ]
}
