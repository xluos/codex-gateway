#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_ROOT="${CODEX_GATEWAY_HOME:-$HOME/.codex-gateway}"
BIN_DIR="$INSTALL_ROOT/bin"
COMPLETION_DIR="$INSTALL_ROOT/completions"
CONFIG_PATH="$INSTALL_ROOT/config.yaml"
BIN_PATH="$BIN_DIR/codexgateway"
SHORTCUT_PATH="$BIN_DIR/cgw"
PATH_EXPORT='export PATH="$HOME/.codex-gateway/bin:$PATH"'
RC_MARKER_BEGIN="# >>> codex-gateway >>>"
RC_MARKER_END="# <<< codex-gateway <<<"
COMPLETION_MARKER_BEGIN="# >>> codex-gateway completion >>>"
COMPLETION_MARKER_END="# <<< codex-gateway completion <<<"

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
  mkdir -p "$INSTALL_ROOT" "$BIN_DIR" "$COMPLETION_DIR"
}

install_config_if_missing() {
  if [[ ! -f "$CONFIG_PATH" ]]; then
    printf '[install] config not found, run "codexgateway init" after installation: %s\n' "$CONFIG_PATH"
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

generate_completion() {
  local shell_name="$1"
  local output_path="$2"
  "$BIN_PATH" completion "$shell_name" >"$output_path"
  printf '[install] wrote %s completion: %s\n' "$shell_name" "$output_path"
}

completion_source_block() {
  local shell_name="$1"
  case "$shell_name" in
    zsh)
      cat <<EOF
$COMPLETION_MARKER_BEGIN
if [ -n "\${ZSH_VERSION:-}" ]; then
  autoload -Uz compinit
  if ! typeset -f compdef >/dev/null 2>&1; then
    compinit
  fi
  source "\$HOME/.codex-gateway/completions/_codexgateway"
fi
$COMPLETION_MARKER_END
EOF
      ;;
    bash)
      cat <<EOF
$COMPLETION_MARKER_BEGIN
if [ -n "\${BASH_VERSION:-}" ]; then
  source "\$HOME/.codex-gateway/completions/codexgateway.bash"
fi
$COMPLETION_MARKER_END
EOF
      ;;
    fish)
      cat <<EOF
$COMPLETION_MARKER_BEGIN
if test -n "\$FISH_VERSION"
  source "\$HOME/.codex-gateway/completions/codexgateway.fish"
end
$COMPLETION_MARKER_END
EOF
      ;;
    *)
      return 1
      ;;
  esac
}

ensure_completion_block() {
  local rc_file="$1"
  local shell_name="$2"
  mkdir -p "$(dirname "$rc_file")"
  touch "$rc_file"

  if grep -Fq "$COMPLETION_MARKER_BEGIN" "$rc_file"; then
    printf '[install] completion block already exists in %s\n' "$rc_file"
    return
  fi

  printf '\n' >>"$rc_file"
  completion_source_block "$shell_name" >>"$rc_file"
  printf '[install] appended completion block to %s\n' "$rc_file"
}

install_completion() {
  local shell_name="$1"
  local rc_file="$2"

  case "$shell_name" in
    zsh)
      generate_completion zsh "$COMPLETION_DIR/_codexgateway"
      ;;
    bash)
      generate_completion bash "$COMPLETION_DIR/codexgateway.bash"
      ;;
    fish)
      generate_completion fish "$COMPLETION_DIR/codexgateway.fish"
      ;;
    *)
      printf '[install] shell %s does not support automatic completion setup, skipping\n' "$shell_name"
      return
      ;;
  esac

  ensure_completion_block "$rc_file" "$shell_name"
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
  install_completion "$(basename "${SHELL:-zsh}")" "$rc_file"
  verify_install

  printf '\n[install] done\n'
  printf 'binary: %s\n' "$BIN_PATH"
  printf 'alias : %s -> %s\n' "$SHORTCUT_PATH" "$BIN_PATH"
  printf 'config: %s\n' "$CONFIG_PATH"
  printf 'shell : %s\n' "$rc_file"
  printf 'completion dir: %s\n' "$COMPLETION_DIR"
  printf '\n'
  printf 'next:\n'
  printf '  source %s\n' "$rc_file"
  printf '  codexgateway init\n'
  printf '  codexgateway help\n'
  printf '  cgw help\n'
}

main "$@"
