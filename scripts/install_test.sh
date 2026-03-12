#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
cleanup() {
  chmod -R u+w "$TMP_DIR" 2>/dev/null || true
  rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup EXIT

assert_contains_once() {
  local file="$1"
  local pattern="$2"
  local count
  count="$(grep -F -c "$pattern" "$file" || true)"
  if [[ "$count" != "1" ]]; then
    printf '[test] expected %s to contain %q exactly once, got %s\n' "$file" "$pattern" "$count" >&2
    exit 1
  fi
}

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
    "$ROOT_DIR/scripts/install.sh"
  HOME="$fake_home" SHELL="/bin/$shell_name" CODEX_GATEWAY_HOME="$install_root" GOCACHE="$go_cache" GOMODCACHE="$go_mod_cache" \
    "$ROOT_DIR/scripts/install.sh"

  [[ -f "$rc_file" ]] || { printf '[test] missing rc file %s\n' "$rc_file" >&2; exit 1; }

  assert_contains_once "$rc_file" "# >>> codex-gateway completion >>>"
  assert_contains_once "$rc_file" "# <<< codex-gateway completion <<<"

  case "$shell_name" in
    zsh) [[ -f "$install_root/completions/_codexgateway" ]] || { printf '[test] missing zsh completion\n' >&2; exit 1; } ;;
    bash) [[ -f "$install_root/completions/codexgateway.bash" ]] || { printf '[test] missing bash completion\n' >&2; exit 1; } ;;
    fish) [[ -f "$install_root/completions/codexgateway.fish" ]] || { printf '[test] missing fish completion\n' >&2; exit 1; } ;;
  esac
}

run_case zsh
run_case bash
run_case fish

printf '[test] install completion checks passed\n'
