#!/usr/bin/env bash
# Servlo installer — https://github.com/ServloOfficial/servlo
# Usage:
#   Install:   curl -fsSL https://raw.githubusercontent.com/ServloOfficial/servlo/main/install.sh | bash
#      or:     wget -qO- https://raw.githubusercontent.com/ServloOfficial/servlo/main/install.sh | bash
#   Update:    servlo-installer --update
#   Uninstall: servlo-installer --uninstall

set -euo pipefail

# ── Constants ────────────────────────────────────────────────────────────────
# REPO is the GitHub owner/name release assets are fetched from; override with
# SERVLO_REPO so a future org move needs no installer change.
REPO="${SERVLO_REPO:-ServloOfficial/servlo}"
BINARY="servlo"
INSTALL_DIR="${SERVLO_INSTALL_DIR:-$HOME/.local/bin}"
SERVLO_CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/servlo"
SERVLO_DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/servlo"

# ── Colors ───────────────────────────────────────────────────────────────────
if [ -t 1 ]; then
  RED='\033[0;31m'; YELLOW='\033[1;33m'; GREEN='\033[0;32m'
  CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'
else
  RED=''; YELLOW=''; GREEN=''; CYAN=''; BOLD=''; RESET=''
fi

# ── Helpers ──────────────────────────────────────────────────────────────────
info()    { echo -e "  ${CYAN}-->${RESET} $*"; }
success() { echo -e "  ${GREEN}✓${RESET}  $*"; }
warn()    { echo -e "  ${YELLOW}!${RESET}  $*"; }
error()   { echo -e "  ${RED}✗${RESET}  $*" >&2; }
die()     { error "$*"; exit 1; }
header()  { echo -e "\n${BOLD}$*${RESET}"; }
ask()     { echo -en "  ${BOLD}?${RESET}  $* [y/N] "; read -r _ans </dev/tty 2>/dev/null || true; [[ "$_ans" =~ ^[Yy]$ ]]; }
star_note() {
  echo ""
  echo -e "  ${CYAN}★${RESET}  If servlo is useful to you, a GitHub star helps others find it:"
  echo -e "     https://github.com/${REPO}"
}

# ── Platform detection ───────────────────────────────────────────────────────
detect_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    *) die "Unsupported OS: $(uname -s). Servlo runs on Ubuntu 24.04 LTS." ;;
  esac
}

detect_arch() {
  # macOS reports arm64; Linux reports aarch64: both map to the arm64 release.
  case "$(uname -m)" in
    x86_64)        echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) die "Unsupported architecture: $(uname -m)" ;;
  esac
}

detect_distro() {
  if [ -f /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    echo "${ID:-unknown}"
  else
    echo "unknown"
  fi
}

# ubuntu_release returns the VERSION_ID from /etc/os-release ("24.04"), empty
# when it cannot be read.
ubuntu_release() {
  if [ -f /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    echo "${VERSION_ID:-}"
  fi
}

# ubuntu_pretty_name returns PRETTY_NAME so a refusal can name what it found
# rather than just what it wanted.
ubuntu_pretty_name() {
  if [ -f /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    echo "${PRETTY_NAME:-$(detect_distro)}"
  else
    detect_distro
  fi
}

# require_ubuntu refuses anything that is not Ubuntu. Derivatives are refused
# too, deliberately: they are not what Servlo is tested on, and a half-install
# on an untested base is worse than a clear refusal.
require_ubuntu() {
  local distro; distro="$(detect_distro)"
  if [ "$distro" != "ubuntu" ]; then
    die "Servlo requires Ubuntu 24.04 LTS.\nThis system is: $(ubuntu_pretty_name)"
  fi
}

# PODMAN_MIN is the oldest podman that understands the quadlet units Servlo
# writes. Ubuntu 22.04 ships 3.4.4, which is why the refusal below points at the
# release upgrade rather than at a podman package that release does not carry.
PODMAN_MIN_MAJOR=4
PODMAN_MIN_MINOR=5

# podman_version echoes the installed podman's version ("4.9.3"), empty when
# podman is not on PATH.
podman_version() {
  command -v podman >/dev/null 2>&1 || return 0
  podman --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -1
}

# require_podman_min refuses a podman older than the quadlet minimum, naming the
# way forward. On a pre-24.04 Ubuntu that way is the release upgrade.
require_podman_min() {
  local ver; ver="$(podman_version)"
  if [ -z "$ver" ]; then
    die "podman is not installed.\nServlo needs podman ${PODMAN_MIN_MAJOR}.${PODMAN_MIN_MINOR} or newer: sudo apt install podman"
  fi
  local major minor
  major="${ver%%.*}"
  minor="${ver#*.}"; minor="${minor%%.*}"
  if [ "$major" -gt "$PODMAN_MIN_MAJOR" ] 2>/dev/null; then return 0; fi
  if [ "$major" -eq "$PODMAN_MIN_MAJOR" ] && [ "$minor" -ge "$PODMAN_MIN_MINOR" ] 2>/dev/null; then return 0; fi

  local release; release="$(ubuntu_release)"
  local msg="podman ${ver} is older than the ${PODMAN_MIN_MAJOR}.${PODMAN_MIN_MINOR} minimum Servlo needs for quadlet units."
  case "$release" in
    24.*|25.*|26.*)
      die "${msg}\nUpgrade podman: sudo apt update && sudo apt install --only-upgrade podman" ;;
    *)
      die "${msg}\nUbuntu ${release:-<unknown>} cannot provide it. Upgrade to 24.04 LTS first:\n  sudo do-release-upgrade" ;;
  esac
}

# require_crun refuses a host without crun. Servlo's quadlets name crun as the
# runtime explicitly, and podman falls back to runc without complaining, so the
# absence does not surface until the first container refuses to start, well
# after the installer has declared success.
require_crun() {
  if command -v crun >/dev/null 2>&1; then
    return 0
  fi
  die "crun is not installed.\nServlo's container units name crun as the runtime: sudo apt install crun"
}

# require_cgroup_v2 refuses the v1 hierarchy. Rootless podman needs the unified
# hierarchy to apply per-container limits at all, so on v1 the memory cap an
# asset build runs under is accepted and silently ignored, which is the failure
# mode that lets an oversized npm build take MySQL down with it.
require_cgroup_v2() {
  local fstype; fstype="$(stat -fc %T /sys/fs/cgroup 2>/dev/null || true)"
  if [ "$fstype" = "cgroup2fs" ]; then
    return 0
  fi
  die "This host is not on cgroup v2 (/sys/fs/cgroup is ${fstype:-unreadable}).\nServlo needs the unified hierarchy for per-container resource limits.\nAdd systemd.unified_cgroup_hierarchy=1 to the kernel command line and reboot."
}

# require_linger refuses a user without linger. Every servlo unit is a systemd
# *user* unit, so without linger the panel, the watcher, nginx and every site
# stop the moment the operator's session ends. Enabling it needs no privilege,
# which is why this prints the command rather than running it.
require_linger() {
  linger_enabled && return 0
  local user; user="$(invoking_user)"
  die "systemd linger is not enabled for ${user}.\nWithout it every Servlo unit stops when you log out. Enable it with:\n  loginctl enable-linger ${user}"
}

# invoking_user names the user the units will belong to. $USER is not exported
# in every context the installer can be piped into, so fall back to the passwd
# entry rather than tripping set -u.
invoking_user() { echo "${USER:-$(id -un)}"; }

# linger_enabled is the predicate behind require_linger, split out so the
# prerequisite pass can offer to fix it before refusing.
linger_enabled() {
  local user; user="$(invoking_user)"
  [ "$(loginctl show-user "$user" --property=Linger 2>/dev/null || true)" = "Linger=yes" ]
}

# ── Prerequisite checks ──────────────────────────────────────────────────────
MISSING_PKGS=()

check_cmd() {
  local cmd="$1" pkg="${2:-$1}" desc="${3:-}"
  if command -v "$cmd" &>/dev/null; then
    success "$cmd found ($(command -v "$cmd"))"
  else
    warn "$cmd not found${desc:+ — $desc}"
    MISSING_PKGS+=("$pkg")
  fi
}

check_systemd_user() {
  if systemctl --user status &>/dev/null 2>&1; then
    success "systemd user session active"
  else
    warn "systemd user session not active — log out and back in if the linger check below fails"
  fi
}

# offer_linger enables linger when it is off and the operator agrees. Enabling
# needs no privilege, and a fresh droplet almost never has it, so refusing
# without offering would send everyone to the same one-line fix by hand.
# require_linger still runs afterwards and refuses if this did not take.
offer_linger() {
  linger_enabled && return 0
  local user; user="$(invoking_user)"
  warn "systemd linger is off for ${user} — Servlo units would stop at logout"
  if ask "Enable systemd linger for ${user} now?"; then
    loginctl enable-linger "$user" || true
  fi
}

check_dns_resolver() {
  if systemctl is-active --quiet NetworkManager 2>/dev/null; then
    success "NetworkManager running"
  elif systemctl is-active --quiet systemd-resolved 2>/dev/null; then
    success "systemd-resolved running"
  else
    warn "No supported DNS resolver running (need NetworkManager or systemd-resolved)"
    MISSING_PKGS+=("networkmanager")
  fi
}

check_podman_rootless() {
  if ! command -v podman &>/dev/null; then
    return  # already flagged by check_cmd
  fi
  if podman info --format '{{.Host.Security.Rootless}}' 2>/dev/null | grep -q true; then
    success "Podman running rootless"
  else
    warn "Podman may not be configured for rootless — check 'podman info'"
  fi
}

check_prerequisites() {
  check_prerequisites_linux
}

check_prerequisites_linux() {
  header "Checking prerequisites"

  # The platform gate runs first: refusing here costs the user nothing, while
  # refusing after packages and downloads is the half-install this prevents.
  require_ubuntu
  success "Ubuntu $(ubuntu_release) detected"

  check_cmd podman podman "container runtime"
  check_cmd unzip unzip "needed to extract fnm"
  require_podman_min
  success "podman $(podman_version) meets the ${PODMAN_MIN_MAJOR}.${PODMAN_MIN_MINOR} minimum"
  require_crun
  success "crun found ($(command -v crun))"
  require_cgroup_v2
  success "cgroup v2 unified hierarchy"
  check_dns_resolver
  check_systemd_user
  offer_linger
  require_linger
  success "systemd linger enabled for $(invoking_user)"
  check_podman_rootless

  if [ ${#MISSING_PKGS[@]} -eq 0 ]; then
    success "All prerequisites met"
    return
  fi

  echo ""
  warn "Missing: ${MISSING_PKGS[*]}"

  local installable=()
  for p in "${MISSING_PKGS[@]}"; do
    [[ "$p" != _* ]] && installable+=("$p")
  done

  if [ ${#installable[@]} -gt 0 ] && ask "Install missing packages now?"; then
    install_packages "${installable[@]}"
  fi

}

install_packages() {
  local pkgs=("$@")

  header "Installing: ${pkgs[*]}"

  sudo apt-get update -q
  sudo apt-get install -y "${pkgs[@]}"

  success "Packages installed"

  # Initialize podman storage for the current user after first install.
  # This runs any pending migrations and sets up ~/.local/share/containers.
  if command -v podman &>/dev/null; then
    podman system migrate &>/dev/null || true
  fi
}

# ── Download tool ────────────────────────────────────────────────────────────
# Prefer curl; fall back to wget. Errors out if neither is available.
_download_tool() {
  if command -v curl &>/dev/null; then
    echo "curl"
  elif command -v wget &>/dev/null; then
    echo "wget"
  else
    die "Neither curl nor wget found. Install one and retry."
  fi
}

# Retry transient failures with a short pause, bound connects, and abort a
# stalled transfer (curl: under 1 byte/s for 30s) so it fails into the retry
# path instead of hanging forever.
CURL_RETRY=(--retry 3 --retry-delay 2 --connect-timeout 15 --speed-limit 1 --speed-time 30)
WGET_RETRY=(--tries=3 --timeout=15)

# fetch <url> <dest>  — download URL to dest file
fetch() {
  local url="$1" dest="$2"
  case "$(_download_tool)" in
    curl) curl -fsSL "${CURL_RETRY[@]}" --progress-bar "$url" -o "$dest" ;;
    wget) wget -q --show-progress "${WGET_RETRY[@]}" "$url" -O "$dest" ;;
  esac
}

# fetch_stdout <url>  — download URL to stdout (for piping into grep/sed)
fetch_stdout() {
  local url="$1"
  case "$(_download_tool)" in
    curl) curl -fsSL "${CURL_RETRY[@]}" "$url" ;;
    wget) wget -qO- "${WGET_RETRY[@]}" "$url" ;;
  esac
}

# ── GitHub release helpers ───────────────────────────────────────────────────
latest_version() {
  # Use the HTML releases/latest redirect — no API key, not rate-limited.
  # GitHub redirects to the canonical release URL whose path contains the tag.
  local url="https://github.com/${REPO}/releases/latest"
  local location
  case "$(_download_tool)" in
    curl) location="$(curl -fsSLI "${CURL_RETRY[@]}" --stderr /dev/null \
            -H "User-Agent: servlo-installer" \
            "$url" | grep -i '^location:' | tail -1)" ;;
    wget) location="$(wget -qS --spider "${WGET_RETRY[@]}" \
            --header "User-Agent: servlo-installer" \
            "$url" 2>&1 | grep -i 'Location:'  | tail -1)" ;;
  esac

  # location header value looks like: .../releases/tag/v0.1.33
  echo "$location" | sed -E 's|.*/releases/tag/v?([^[:space:]]+).*|\1|' | tr -d '\r'
}

# download_binary <version> <arch> <destdir>
# Downloads and extracts the release archive into <destdir>.
# The extracted binary will be at <destdir>/servlo.
# All output goes to stderr — nothing is printed to stdout.
download_binary() {
  local version="$1" arch="$2" destdir="$3"
  local os; os="$(detect_os)"
  local filename="servlo_${version}_${os}_${arch}.tar.gz"
  local url="https://github.com/${REPO}/releases/download/v${version}/${filename}"

  info "Downloading servlo v${version} (${arch}) via $(_download_tool) ..."
  if ! fetch "$url" "${destdir}/${filename}"; then
    die "Download failed (HTTP 404).\nNo release v${version} found at:\n  ${url}\n\nIf you built servlo locally, use:\n  bash install.sh --local ./build/servlo"
  fi

  if ! tar -xzf "${destdir}/${filename}" -C "$destdir" 2>&1; then
    die "Failed to extract archive: ${filename}"
  fi
}

installed_version() {
  if command -v servlo &>/dev/null; then
    servlo --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "unknown"
  else
    echo ""
  fi
}

# Full version token including any git-describe suffix (e.g.
# 1.25.0-6-g7d030096-dirty). installed_version() collapses that to the bare
# 1.25.0, which is what version_is_dev needs the suffix from.
installed_version_raw() {
  if command -v servlo &>/dev/null; then
    servlo --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+[A-Za-z0-9.-]*' | head -1 || echo ""
  else
    echo ""
  fi
}

# True when the version token carries a git-describe suffix. Release binaries
# report a clean X.Y.Z, so a suffix means an ahead-of-release local build that
# the installer must not silently overwrite with the matching base release.
version_is_dev() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+- ]]
}

# Guards an overwrite of a local development build. Prints a warning and asks
# before replacing it; keeps the build and exits 0 when declined or when no tty
# is available to confirm. $1 is the release version about to be installed.
guard_dev_build() {
  local target="$1" current_raw; current_raw="$(installed_version_raw)"
  version_is_dev "$current_raw" || return 0
  warn "A local development build (v${current_raw}) is installed."
  if [ -r /dev/tty ] && ask "Replace it with release v${target}?"; then
    return 0
  fi
  info "Keeping your local build. Reinstall a dev build with: install.sh --local <path>"
  exit 0
}

# ── Shell integration ────────────────────────────────────────────────────────
SHELL_MARKER="# Added by Servlo installer"

detect_shell_rc() {
  local shell; shell="$(basename "${SHELL:-bash}")"
  case "$shell" in
    fish) echo "$HOME/.config/fish/conf.d/servlo.fish" ;;
    zsh)  echo "$HOME/.zshrc" ;;
    *) echo "$HOME/.bashrc" ;;
  esac
}

add_to_path() {
  local shell; shell="$(basename "${SHELL:-bash}")"
  local rc; rc="$(detect_shell_rc)"

  # Check if already in current PATH
  if [[ ":$PATH:" == *":$INSTALL_DIR:"* ]]; then
    success "$INSTALL_DIR is already in PATH"
    return
  fi

  # Don't add if already present in rc file
  if grep -q "$SHELL_MARKER" "$rc" 2>/dev/null; then
    success "PATH already configured in $rc"
    return
  fi

  case "$shell" in
    fish)
      mkdir -p "$(dirname "$rc")"
      printf '\n%s\nfish_add_path %s\n' "$SHELL_MARKER" "$INSTALL_DIR" >> "$rc"
      ;;
    *)
      printf '\n%s\nexport PATH="%s:$PATH"\n' "$SHELL_MARKER" "$INSTALL_DIR" >> "$rc"
      ;;
  esac
  success "Added $INSTALL_DIR to PATH in $rc"
  warn "Reload your shell or run: source $rc"
}

remove_from_path() {
  local rc; rc="$(detect_shell_rc)"
  if [ ! -f "$rc" ]; then return; fi

  # Remove the block: marker line + the next line. {N;d;} is POSIX and works on
  # both GNU and BSD/macOS sed, unlike the GNU-only `,+1` relative address.
  if grep -q "$SHELL_MARKER" "$rc" 2>/dev/null; then
    sed -i.bak -e "/^${SHELL_MARKER}/{N;d;}" "$rc" && rm -f "${rc}.bak"
    info "Removed PATH entry from $rc"
  fi
}

# ── Install ──────────────────────────────────────────────────────────────────
cmd_install() {
  local local_binary="${1:-}"
  header "Installing Servlo"

  # Validate local binary path before running any checks so the error is clear.
  if [ -n "$local_binary" ]; then
    [ -f "$local_binary" ] || die "File not found: $local_binary"
  fi

  check_prerequisites

  if ! command -v podman &>/dev/null; then
    die "podman is required but not installed. Install it and re-run this script."
  fi

  mkdir -p "$INSTALL_DIR"

  if [ -n "$local_binary" ]; then
    # ── Local binary path supplied (e.g. ./build/servlo) ──
    [ -f "$local_binary" ] || die "File not found: $local_binary"
    install -m 755 "$local_binary" "${INSTALL_DIR}/${BINARY}"
    local version; version="$("${INSTALL_DIR}/${BINARY}" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "dev")"
    success "Installed servlo ${version} (local) → ${INSTALL_DIR}/${BINARY}"
  else
    # ── Download from GitHub releases ──
    local arch; arch="$(detect_arch)"
    local version; version="$(latest_version)"
    if [ -z "$version" ]; then
      die "No releases found at https://github.com/${REPO}/releases\n\nIf you built servlo locally, install with:\n  bash install.sh --local ./build/servlo"
    fi

    local current; current="$(installed_version)"
    if [ -n "$current" ] && [ "$current" = "$version" ]; then
      success "Servlo v${version} is already installed and up to date"
      exit 0
    fi
    guard_dev_build "$version"

    local tmpdir; tmpdir="$(mktemp -d)"
    download_binary "$version" "$arch" "$tmpdir"
    install -m 755 "${tmpdir}/servlo" "${INSTALL_DIR}/${BINARY}"
    rm -rf "$tmpdir"
    success "Installed servlo v${version} → ${INSTALL_DIR}/${BINARY}"
  fi

  add_to_path

  echo ""
  info "Running 'servlo install' to complete setup ..."
  echo ""
  # When this script is piped through `curl|bash`, our own stdin is the pipe
  # and servlo's prompts would silently hit EOF. Hand it /dev/tty when one is
  # available so [Y/n] questions reach the user.
  if [ -r /dev/tty ]; then
    "${INSTALL_DIR}/${BINARY}" install </dev/tty
  else
    "${INSTALL_DIR}/${BINARY}" install
  fi

  star_note
}

# ── Update ───────────────────────────────────────────────────────────────────
cmd_update() {
  header "Updating Servlo"

  local arch; arch="$(detect_arch)"
  local latest; latest="$(latest_version)"
  [ -n "$latest" ] || die "Could not fetch latest version."

  local current; current="$(installed_version)"

  if [ "$current" = "$latest" ]; then
    success "Already on latest: v${latest}"
    exit 0
  fi
  guard_dev_build "$latest"

  info "Updating v${current:-unknown} → v${latest}"
  local tmpdir; tmpdir="$(mktemp -d)"
  download_binary "$latest" "$arch" "$tmpdir"
  install -m 755 "${tmpdir}/servlo" "${INSTALL_DIR}/${BINARY}"
  rm -rf "$tmpdir"
  success "Updated to servlo v${latest}"
  star_note
}

# ── Uninstall ────────────────────────────────────────────────────────────────
# The root-owned files an older servlo's managed DNS wrote. Servlo no longer
# writes any of them, but a machine installed before that removal still carries
# them, including a passwordless sudoers grant nothing uses any more, so the
# uninstaller still detects the set and offers to clear it.
SERVLO_DNS_FILES=(
  /etc/sudoers.d/servlo
  /etc/systemd/system/servlo-dns-link.service
  /etc/systemd/resolved.conf.d/servlo-fallback.conf
  /etc/systemd/resolved.conf.d/servlo.conf
  /etc/NetworkManager/conf.d/servlo-dns-link.conf
  /etc/NetworkManager/conf.d/servlo.conf
  /etc/NetworkManager/dnsmasq.d/servlo.conf
  /etc/NetworkManager/dispatcher.d/99-servlo-dns
)

servlo_dns_config_present() {
  local f
  for f in "${SERVLO_DNS_FILES[@]}"; do
    [ -e "$f" ] && return 0
  done
  return 1
}

servlo_dns_cleanup_hint() {
  warn "Servlo's DNS configuration is still on this system and only root can remove it:"
  info "sudo systemctl disable --now servlo-dns-link.service"
  info "sudo rm -f ${SERVLO_DNS_FILES[*]}"
  info "sudo systemctl daemon-reload && sudo systemctl restart systemd-resolved"
  info "If the interface is still up: sudo ip link del servlo0"
}

# Left in place, the link unit recreates servlo0 at every boot pointing .test at a
# dnsmasq that no longer exists, and the fallback drop-in keeps systemd-resolved's
# fallback servers switched off for good. The binary is removed further down and
# it is the only thing that can undo any of it, so offer the teardown here.
uninstall_linux_dns() {
  servlo_dns_config_present || return 0

  if ! command -v servlo &>/dev/null; then
    servlo_dns_cleanup_hint
    return 0
  fi

  warn "The system DNS setup (the servlo0 link, its root unit, the NetworkManager rules and systemd-resolved's fallback servers) is removed only by servlo itself."
  info "Nothing on this machine can undo it once the binary is gone."
  if ask "Remove the DNS setup now? (runs 'servlo dns:disable', needs sudo)"; then
    if servlo dns:disable; then
      success "Removed servlo DNS configuration"
      return 0
    fi
    warn "'servlo dns:disable' did not complete."
  fi
  servlo_dns_cleanup_hint
}

cmd_uninstall() {
  header "Uninstalling Servlo"

  uninstall_linux_dns

  # Stop and remove systemd units — discover from quadlet files on disk
  local quadlet_dir="${XDG_CONFIG_HOME:-$HOME/.config}/containers/systemd"
  local systemd_user_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

  if [ -d "$quadlet_dir" ]; then
    for f in "$quadlet_dir"/servlo-*.container; do
      [ -f "$f" ] || continue
      local unit; unit="$(basename "$f" .container)"
      if systemctl --user is-active --quiet "$unit" 2>/dev/null; then
        info "Stopping $unit ..."
        systemctl --user stop "$unit" 2>/dev/null || true
      fi
      systemctl --user disable "$unit" 2>/dev/null || true
    done
    rm -f "$quadlet_dir"/servlo-*.container
    info "Removed Quadlet units from $quadlet_dir"
  fi

  # Stop and remove user service unit files
  for svc in servlo-watcher servlo-panel; do
    if systemctl --user is-active --quiet "$svc" 2>/dev/null; then
      systemctl --user stop "$svc" 2>/dev/null || true
    fi
    systemctl --user disable "$svc" 2>/dev/null || true
    rm -f "$systemd_user_dir/${svc}.service"
  done

  systemctl --user daemon-reload 2>/dev/null || true

  # Remove binary
  if [ -f "${INSTALL_DIR}/${BINARY}" ]; then
    rm -f "${INSTALL_DIR}/${BINARY}"
    success "Removed ${INSTALL_DIR}/${BINARY}"
  fi

  # Remove PATH entry from shell rc
  remove_from_path

  # Optionally remove data
  if ask "Remove all Servlo data and config? (~/.config/servlo, ~/.local/share/servlo)"; then
    rm -rf "$SERVLO_CONFIG_DIR"
    rm -rf "$SERVLO_DATA_DIR"
    success "Removed config and data directories"
  else
    info "Config kept at $SERVLO_CONFIG_DIR"
    info "Data kept at $SERVLO_DATA_DIR"
  fi

  success "Servlo uninstalled"
}

# ── Entry point ──────────────────────────────────────────────────────────────
main() {
  echo -e "${BOLD}"
  echo "  ███████╗███████╗██████╗ ██╗   ██╗██╗      ██████╗ "
  echo "  ██╔════╝██╔════╝██╔══██╗██║   ██║██║     ██╔═══██╗"
  echo "  ███████╗█████╗  ██████╔╝██║   ██║██║     ██║   ██║"
  echo "  ╚════██║██╔══╝  ██╔══██╗╚██╗ ██╔╝██║     ██║   ██║"
  echo "  ███████║███████╗██║  ██║ ╚████╔╝ ███████╗╚██████╔╝"
  echo "  ╚══════╝╚══════╝╚═╝  ╚═╝  ╚═══╝  ╚══════╝ ╚═════╝ "
  echo -e "${RESET}"
  echo "  Servlo — Podman-powered PHP server panel for Ubuntu 24.04 LTS"
  echo "  https://github.com/ServloOfficial/servlo"
  echo ""

  case "${1:-install}" in
    --update|-u|update)     cmd_update ;;
    --uninstall|uninstall)  cmd_uninstall ;;
    --check|check)
      # Non-interactive: report the full requirement set (managed DNS), so the
      # check never blocks on a prompt. Picking .localhost at real install time
      # is what skips the HTTPS-only packages.
      MISSING_PKGS=()
      check_prerequisites
      ;;
    --local)
      [ -n "${2:-}" ] || die "--local requires a path argument, e.g: --local ./build/servlo"
      cmd_install "$2"
      ;;
    --help|-h)
      echo "Usage: $0 [--update | --uninstall | --check | --local <path>]"
      echo ""
      echo "  (no args)       Install Servlo from latest GitHub release"
      echo "  --local <path>  Install from a locally built binary"
      echo "  --update        Update to the latest release"
      echo "  --uninstall     Remove Servlo and optionally its data"
      echo "  --check         Check prerequisites only"
      ;;
    --install|install|"") cmd_install ;;
    *) die "Unknown option: $1. Run with --help for usage." ;;
  esac
}

# Only run main when executed directly or piped to bash, not when sourced.
# BASH_SOURCE may be an unset array when piped to bash (curl|bash / wget|bash),
# which triggers set -u on some bash versions even with the :- operator.
# Suspend nounset briefly to read it safely.
set +u
_servlo_src="${BASH_SOURCE[0]:-}"
set -u
if [[ -z "$_servlo_src" || "$_servlo_src" == "$0" ]]; then
  main "$@"
fi
unset _servlo_src
