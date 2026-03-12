#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
cleanup() {
  chmod -R u+w "$TMP_DIR" 2>/dev/null || true
  rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup EXIT

run_case() {
  local shell_name="$1"
  local fake_home="$TMP_DIR/$shell_name-home"
  local install_root="$fake_home/.codex-gateway"
  local go_cache="$TMP_DIR/$shell_name-gocache"
  local go_mod_cache="$TMP_DIR/$shell_name-gomodcache"
  local rc_file

  mkdir -p "$fake_home"

  case "$shell_name" in
    zsh) rc_file="$fake_home/.zshrc" ;;
    bash) rc_file="$fake_home/.bashrc" ;;
    fish) rc_file="$fake_home/.config/fish/config.fish" ;;
    *)
      printf '[test] unsupported shell %s\n' "$shell_name" >&2
      exit 1
      ;;
  esac

  HOME="$fake_home" SHELL="/bin/$shell_name" CODEX_GATEWAY_HOME="$install_root" GOCACHE="$go_cache" GOMODCACHE="$go_mod_cache" \
    "$ROOT_DIR/scripts/build.sh"
  HOME="$fake_home" SHELL="/bin/$shell_name" CODEX_GATEWAY_HOME="$install_root" GOCACHE="$go_cache" GOMODCACHE="$go_mod_cache" \
    "$ROOT_DIR/scripts/build.sh"

  [[ -x "$install_root/bin/codexgateway" ]] || { printf '[test] missing binary %s\n' "$install_root/bin/codexgateway" >&2; exit 1; }
  [[ -L "$install_root/bin/cgw" ]] || { printf '[test] missing shortcut symlink %s\n' "$install_root/bin/cgw" >&2; exit 1; }
  [[ ! -e "$rc_file" ]] || { printf '[test] rc file should not be created: %s\n' "$rc_file" >&2; exit 1; }

  [[ "$(readlink "$install_root/bin/cgw")" == "$install_root/bin/codexgateway" ]] || {
    printf '[test] shortcut symlink target is incorrect for %s\n' "$shell_name" >&2
    exit 1
  }
}

run_case zsh
run_case bash
run_case fish

printf '[test] local build checks passed\n'
