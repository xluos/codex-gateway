#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
SERVER_PID=""

cleanup() {
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  chmod -R u+w "$TMP_DIR" 2>/dev/null || true
  rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup EXIT

create_fake_release() {
  local version="$1"
  local releases_dir="$TMP_DIR/server/releases/download/$version"
  local api_dir="$TMP_DIR/server/api/repos/test-owner/test-repo/releases"
  mkdir -p "$releases_dir" "$api_dir/tags"

  cat >"$TMP_DIR/codexgateway" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "help" ]]; then
  echo "codexgateway fake help"
  exit 0
fi
echo "codexgateway fake binary"
EOF
  chmod +x "$TMP_DIR/codexgateway"

  local assets=(
    "codexgateway_darwin_amd64.tar.gz"
    "codexgateway_darwin_arm64.tar.gz"
    "codexgateway_linux_amd64.tar.gz"
    "codexgateway_linux_arm64.tar.gz"
  )

  : >"$releases_dir/checksums.txt"
  for asset in "${assets[@]}"; do
    tar -czf "$releases_dir/$asset" -C "$TMP_DIR" codexgateway
    shasum -a 256 "$releases_dir/$asset" >>"$releases_dir/checksums.txt"
  done

  cat >"$api_dir/latest" <<EOF
{"tag_name":"$version"}
EOF
  cat >"$api_dir/tags/$version" <<EOF
{"tag_name":"$version"}
EOF
}

start_server() {
  (
    cd "$TMP_DIR/server"
    python3 -m http.server 18765 >/dev/null 2>&1
  ) &
  SERVER_PID="$!"
  sleep 1
}

assert_file_exists() {
  local path="$1"
  [[ -e "$path" ]] || {
    printf '[test] missing expected path: %s\n' "$path" >&2
    exit 1
  }
}

main() {
  local fake_home="$TMP_DIR/home"
  local install_root="$fake_home/.codex-gateway"
  mkdir -p "$fake_home"

  create_fake_release "v9.9.9"
  start_server

  HOME="$fake_home" \
  CODEX_GATEWAY_HOME="$install_root" \
  REPO_OWNER="test-owner" \
  REPO_NAME="test-repo" \
  GITHUB_API_BASE="http://127.0.0.1:18765/api" \
  GITHUB_DOWNLOAD_BASE="http://127.0.0.1:18765/releases/download" \
  "$ROOT_DIR/install.sh"

  assert_file_exists "$install_root/bin/codexgateway"
  assert_file_exists "$install_root/bin/cgw"
  [[ "$(readlink "$install_root/bin/cgw")" == "$install_root/bin/codexgateway" ]] || {
    printf '[test] cgw symlink target mismatch\n' >&2
    exit 1
  }

  HOME="$fake_home" \
  CODEX_GATEWAY_HOME="$install_root" \
  VERSION="v9.9.9" \
  REPO_OWNER="test-owner" \
  REPO_NAME="test-repo" \
  GITHUB_API_BASE="http://127.0.0.1:18765/api" \
  GITHUB_DOWNLOAD_BASE="http://127.0.0.1:18765/releases/download" \
  "$ROOT_DIR/install.sh"

  printf '[test] release installer checks passed\n'
}

main "$@"
