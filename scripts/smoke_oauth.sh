#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_CONFIG_PATH="${HOME:-$ROOT_DIR}/.codex-gateway/config.yaml"
CONFIG_PATH="${1:-$DEFAULT_CONFIG_PATH}"
LOCAL_API_KEY="${2:-${LOCAL_API_KEY:-}}"
BASE_URL="${3:-${BASE_URL:-http://127.0.0.1:9867}}"
LOG_FILE="${LOG_FILE:-$(mktemp -t codex-gateway-smoke.XXXXXX.log)}"

if [[ -z "$LOCAL_API_KEY" ]]; then
  echo "missing local api key: pass it as the second argument or LOCAL_API_KEY env" >&2
  exit 1
fi

SERVER_PID=""

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if curl --silent --show-error "$BASE_URL/healthz" >/dev/null 2>&1; then
  echo "[smoke] another service is already responding on $BASE_URL; stop it first or choose a different BASE_URL" >&2
  exit 1
fi

echo "[smoke] starting gateway with config: $CONFIG_PATH"
(cd "$ROOT_DIR" && go run ./cmd/server -config "$CONFIG_PATH" >"$LOG_FILE" 2>&1) &
SERVER_PID="$!"

for _ in $(seq 1 30); do
  if curl --silent --show-error "$BASE_URL/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! curl --silent --show-error "$BASE_URL/healthz" >/dev/null 2>&1; then
  echo "[smoke] server did not become healthy" >&2
  tail -n 50 "$LOG_FILE" >&2 || true
  exit 1
fi

echo "[smoke] GET $BASE_URL/v1/models"
models_body="$(mktemp -t codex-gateway-models.XXXXXX.json)"
models_status="$(curl --silent --show-error --output "$models_body" --write-out '%{http_code}' \
  "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $LOCAL_API_KEY")"
cat "$models_body"
echo

if [[ "$models_status" != "200" ]]; then
  echo "[smoke] models request failed with status $models_status" >&2
  tail -n 80 "$LOG_FILE" >&2 || true
  exit 1
fi

echo "[smoke] POST $BASE_URL/v1/responses"
responses_body="$(mktemp -t codex-gateway-responses.XXXXXX.txt)"
responses_status="$(curl --silent --show-error --no-buffer --max-time 90 --output "$responses_body" --write-out '%{http_code}' \
  "$BASE_URL/v1/responses" \
  -H "Authorization: Bearer $LOCAL_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.1-codex","input":"Reply with exactly OK."}')"
sed -n '1,40p' "$responses_body"
echo

if [[ "$responses_status" != "200" ]]; then
  echo "[smoke] responses request failed with status $responses_status" >&2
  tail -n 80 "$LOG_FILE" >&2 || true
  exit 1
fi

if ! grep -q '^data:' "$responses_body"; then
  echo "[smoke] responses stream did not contain SSE data events" >&2
  tail -n 80 "$LOG_FILE" >&2 || true
  exit 1
fi

echo "[smoke] POST $BASE_URL/v1/chat/completions"
chat_body="$(mktemp -t codex-gateway-chat.XXXXXX.json)"
chat_status="$(curl --silent --show-error --max-time 90 --output "$chat_body" --write-out '%{http_code}' \
  "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $LOCAL_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.1-codex","stream":false,"messages":[{"role":"system","content":"You must answer with exactly OK."},{"role":"user","content":"Say something."}]}')"
cat "$chat_body"
echo

if [[ "$chat_status" != "200" ]]; then
  echo "[smoke] chat/completions request failed with status $chat_status" >&2
  tail -n 80 "$LOG_FILE" >&2 || true
  exit 1
fi

if ! grep -q '"chat.completion"' "$chat_body"; then
  echo "[smoke] chat/completions response did not look like chat completion JSON" >&2
  tail -n 80 "$LOG_FILE" >&2 || true
  exit 1
fi

echo "[smoke] recent gateway logs"
tail -n 40 "$LOG_FILE"
