#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_ROOT="${CODEX_GATEWAY_HOME:-$HOME/.codex-gateway}"
BIN_DIR="$INSTALL_ROOT/bin"
CONFIG_PATH="$INSTALL_ROOT/config.yaml"
BIN_PATH="$BIN_DIR/codexgateway"
SHORTCUT_PATH="$BIN_DIR/cgw"
PATH_EXPORT='export PATH="$HOME/.codex-gateway/bin:$PATH"'
RC_MARKER_BEGIN="# >>> codex-gateway >>>"
RC_MARKER_END="# <<< codex-gateway <<<"

detect_rc_file() {
  local shell_name
  shell_name="$(basename "${SHELL:-zsh}")"
  case "$shell_name" in
    zsh) printf '%s\n' "$HOME/.zshrc" ;;
    bash) printf '%s\n' "$HOME/.bashrc" ;;
    fish) printf '%s\n' "$HOME/.config/fish/config.fish" ;;
    *) printf '%s\n' "$HOME/.zshrc" ;;
  esac
}

ensure_dirs() {
  mkdir -p "$INSTALL_ROOT" "$BIN_DIR"
}

install_config_if_missing() {
  if [[ ! -f "$CONFIG_PATH" ]]; then
    cp "$ROOT_DIR/config.example.yaml" "$CONFIG_PATH"
    chmod 600 "$CONFIG_PATH"
    printf '[install] created default config: %s\n' "$CONFIG_PATH"
  fi
}

build_binary() {
  printf '[install] building codexgateway...\n'
  (
    cd "$ROOT_DIR"
    go build -buildvcs=false -o "$BIN_PATH" ./cmd/server
  )
  chmod 755 "$BIN_PATH"
  ln -sf "$BIN_PATH" "$SHORTCUT_PATH"
}

ensure_path_block() {
  local rc_file="$1"
  mkdir -p "$(dirname "$rc_file")"
  touch "$rc_file"

  if grep -Fq "$RC_MARKER_BEGIN" "$rc_file"; then
    printf '[install] PATH block already exists in %s\n' "$rc_file"
    return
  fi

  {
    printf '\n%s\n' "$RC_MARKER_BEGIN"
    printf '%s\n' "$PATH_EXPORT"
    printf '%s\n' "$RC_MARKER_END"
  } >>"$rc_file"
  printf '[install] appended PATH block to %s\n' "$rc_file"
}

verify_install() {
  printf '[install] verifying binary...\n'
  "$BIN_PATH" help >/dev/null
}

main() {
  local rc_file
  rc_file="$(detect_rc_file)"

  ensure_dirs
  install_config_if_missing
  build_binary
  ensure_path_block "$rc_file"
  verify_install

  printf '\n[install] done\n'
  printf 'binary: %s\n' "$BIN_PATH"
  printf 'alias : %s -> %s\n' "$SHORTCUT_PATH" "$BIN_PATH"
  printf 'config: %s\n' "$CONFIG_PATH"
  printf 'shell : %s\n' "$rc_file"
  printf '\n'
  printf 'next:\n'
  printf '  source %s\n' "$rc_file"
  printf '  codexgateway help\n'
  printf '  cgw help\n'
}

main "$@"
