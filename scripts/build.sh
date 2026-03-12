#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_ROOT="${CODEX_GATEWAY_HOME:-$HOME/.codex-gateway}"
BIN_DIR="$INSTALL_ROOT/bin"
BIN_PATH="$BIN_DIR/codexgateway"
SHORTCUT_PATH="$BIN_DIR/cgw"

ensure_dirs() {
  mkdir -p "$INSTALL_ROOT" "$BIN_DIR"
}

build_binary() {
  printf '[build] building codexgateway...\n'
  (
    cd "$ROOT_DIR"
    go build -buildvcs=false -o "$BIN_PATH" ./cmd/server
  )
  chmod 755 "$BIN_PATH"
  ln -sf "$BIN_PATH" "$SHORTCUT_PATH"
}

verify_binary() {
  printf '[build] verifying binary...\n'
  "$BIN_PATH" help >/dev/null
}

main() {
  ensure_dirs
  build_binary
  verify_binary

  printf '\n[build] done\n'
  printf 'binary: %s\n' "$BIN_PATH"
  printf 'alias : %s -> %s\n' "$SHORTCUT_PATH" "$BIN_PATH"
  printf '\n'
  printf 'next:\n'
  printf '  codexgateway help\n'
  printf '  cgw help\n'
}

main "$@"
