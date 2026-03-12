#!/usr/bin/env bash
set -euo pipefail

INSTALL_ROOT="${CODEX_GATEWAY_HOME:-$HOME/.codex-gateway}"
BIN_DIR="$INSTALL_ROOT/bin"
BIN_PATH="$BIN_DIR/codexgateway"
SHORTCUT_PATH="$BIN_DIR/cgw"
REPO_OWNER="${REPO_OWNER:-bytedance}"
REPO_NAME="${REPO_NAME:-codex-gateway}"
GITHUB_API_BASE="${GITHUB_API_BASE:-https://api.github.com}"
GITHUB_DOWNLOAD_BASE="${GITHUB_DOWNLOAD_BASE:-https://github.com}"
VERSION="${VERSION:-}"
TMP_DIR=""

cleanup() {
  if [[ -n "$TMP_DIR" && -d "$TMP_DIR" ]]; then
    chmod -R u+w "$TMP_DIR" 2>/dev/null || true
    rm -rf "$TMP_DIR" 2>/dev/null || true
  fi
}
trap cleanup EXIT

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    printf '[install] required command not found: %s\n' "$1" >&2
    exit 1
  }
}

sha256_file() {
  local path="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$path" | awk '{print $1}'
    return
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
    return
  fi

  printf '[install] neither shasum nor sha256sum is available\n' >&2
  exit 1
}

detect_os() {
  case "$(uname -s)" in
    Darwin) printf '%s\n' "darwin" ;;
    Linux) printf '%s\n' "linux" ;;
    *)
      printf '[install] unsupported operating system: %s\n' "$(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf '%s\n' "amd64" ;;
    arm64|aarch64) printf '%s\n' "arm64" ;;
    *)
      printf '[install] unsupported architecture: %s\n' "$(uname -m)" >&2
      exit 1
      ;;
  esac
}

resolve_version() {
  local response

  if [[ -n "$VERSION" ]]; then
    printf '%s\n' "$VERSION"
    return
  fi

  response="$(curl -fsSL "$GITHUB_API_BASE/repos/$REPO_OWNER/$REPO_NAME/releases/latest")" || {
    printf '[install] failed to resolve latest release from GitHub API\n' >&2
    exit 1
  }

  VERSION="$(printf '%s' "$response" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  if [[ -z "$VERSION" ]]; then
    printf '[install] could not parse tag_name from latest release response\n' >&2
    exit 1
  fi

  printf '%s\n' "$VERSION"
}

asset_url() {
  local version="$1"
  local asset_name="$2"

  if [[ "$GITHUB_DOWNLOAD_BASE" == "https://github.com" ]]; then
    printf '%s\n' "$GITHUB_DOWNLOAD_BASE/$REPO_OWNER/$REPO_NAME/releases/download/$version/$asset_name"
    return
  fi

  printf '%s\n' "$GITHUB_DOWNLOAD_BASE/$version/$asset_name"
}

download_file() {
  local url="$1"
  local output_path="$2"
  curl -fsSL "$url" -o "$output_path" || {
    printf '[install] failed to download %s\n' "$url" >&2
    exit 1
  }
}

verify_checksum() {
  local asset_name="$1"
  local archive_path="$2"
  local checksums_path="$3"
  local expected actual

  expected="$(awk -v name="$asset_name" '
    {
      file = $2
      sub(/^.*\//, "", file)
      if (file == name) {
        print $1
      }
    }
  ' "$checksums_path")"
  if [[ -z "$expected" ]]; then
    printf '[install] checksum entry not found for %s\n' "$asset_name" >&2
    exit 1
  fi

  actual="$(sha256_file "$archive_path")"
  if [[ "$expected" != "$actual" ]]; then
    printf '[install] checksum mismatch for %s\n' "$asset_name" >&2
    printf '[install] expected: %s\n' "$expected" >&2
    printf '[install] actual  : %s\n' "$actual" >&2
    exit 1
  fi
}

install_binary() {
  local archive_path="$1"
  local extract_dir="$2"
  mkdir -p "$BIN_DIR" "$extract_dir"
  tar -xzf "$archive_path" -C "$extract_dir" || {
    printf '[install] failed to extract archive %s\n' "$archive_path" >&2
    exit 1
  }

  [[ -f "$extract_dir/codexgateway" ]] || {
    printf '[install] archive did not contain codexgateway\n' >&2
    exit 1
  }

  install -m 755 "$extract_dir/codexgateway" "$BIN_PATH"
  ln -sf "$BIN_PATH" "$SHORTCUT_PATH"
}

verify_install() {
  "$BIN_PATH" help >/dev/null || {
    printf '[install] installed binary failed verification\n' >&2
    exit 1
  }
}

main() {
  local os arch version asset_name archive_url checksums_url archive_path checksums_path extract_dir

  require_cmd curl
  require_cmd tar
  require_cmd mktemp
  require_cmd uname
  require_cmd install
  if ! command -v shasum >/dev/null 2>&1 && ! command -v sha256sum >/dev/null 2>&1; then
    printf '[install] required command not found: shasum or sha256sum\n' >&2
    exit 1
  fi

  os="$(detect_os)"
  arch="$(detect_arch)"
  version="$(resolve_version)"
  asset_name="codexgateway_${os}_${arch}.tar.gz"
  archive_url="$(asset_url "$version" "$asset_name")"
  checksums_url="$(asset_url "$version" "checksums.txt")"

  TMP_DIR="$(mktemp -d)"
  archive_path="$TMP_DIR/$asset_name"
  checksums_path="$TMP_DIR/checksums.txt"
  extract_dir="$TMP_DIR/extracted"

  printf '[install] version: %s\n' "$version"
  printf '[install] platform: %s/%s\n' "$os" "$arch"
  printf '[install] downloading: %s\n' "$archive_url"
  download_file "$archive_url" "$archive_path"
  download_file "$checksums_url" "$checksums_path"
  verify_checksum "$asset_name" "$archive_path" "$checksums_path"
  install_binary "$archive_path" "$extract_dir"
  verify_install

  printf '\n[install] done\n'
  printf 'binary: %s\n' "$BIN_PATH"
  printf 'alias : %s -> %s\n' "$SHORTCUT_PATH" "$BIN_PATH"
  printf '\n'
  printf 'next:\n'
  printf '  codexgateway init\n'
  printf '  codexgateway help\n'
  printf '  cgw help\n'
}

main "$@"
