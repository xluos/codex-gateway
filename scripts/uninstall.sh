#!/usr/bin/env bash
set -euo pipefail

INSTALL_ROOT="${CODEX_GATEWAY_HOME:-$HOME/.codex-gateway}"
BIN_PATH="$INSTALL_ROOT/bin/codexgateway"
SHORTCUT_PATH="$INSTALL_ROOT/bin/cgw"
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

remove_path_block() {
  local rc_file="$1"
  [[ -f "$rc_file" ]] || return 0
  python3 - <<'PY' "$rc_file" "$RC_MARKER_BEGIN" "$RC_MARKER_END"
import sys
path, begin, end = sys.argv[1:4]
with open(path, 'r', encoding='utf-8') as f:
    lines = f.readlines()
out = []
skip = False
for line in lines:
    stripped = line.rstrip('\n')
    if stripped == begin:
        skip = True
        continue
    if stripped == end:
        skip = False
        continue
    if not skip:
        out.append(line)
with open(path, 'w', encoding='utf-8') as f:
    f.writelines(out)
PY
}

main() {
  local rc_file
  rc_file="$(detect_rc_file)"

  rm -f "$BIN_PATH" "$SHORTCUT_PATH"
  remove_path_block "$rc_file"

  printf '[uninstall] removed binary shortcuts from %s\n' "$INSTALL_ROOT"
  printf '[uninstall] removed PATH block from %s\n' "$rc_file"
  printf '\n'
  printf 'next:\n'
  printf '  source %s\n' "$rc_file"
  printf '  rm -rf %s    # optional: remove config/logs/runtime data\n' "$INSTALL_ROOT"
}

main "$@"
