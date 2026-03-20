#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/docker/status_writer.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

STATUS_FILE="${tmpdir}/status.json"
error_msg=$'line 1\nline 2: model_not_found'

output="$(write_status 1 false false "" "$error_msg" "" 1.25 42)"
expected="BACKFLOW_STATUS_JSON:$(jq -c . "$STATUS_FILE")"

if ! jq -e '
  .exit_code == 1 and
  .complete == false and
  .needs_input == false and
  .question == "" and
  .error == "line 1\nline 2: model_not_found" and
  .pr_url == "" and
  .cost_usd == 1.25 and
  .elapsed_time_sec == 42
' "$STATUS_FILE" >/dev/null; then
  echo "status.json did not match expected content" >&2
  exit 1
fi

if [ "$output" != "$expected" ]; then
  echo "unexpected BACKFLOW_STATUS_JSON output" >&2
  printf 'got:      %s\n' "$output" >&2
  printf 'expected: %s\n' "$expected" >&2
  exit 1
fi

echo "ok"
