#!/usr/bin/env bash
set -euo pipefail

# Tests for reader-entrypoint.sh. Validates pre-flight behavior (env vars,
# auth) and the result-parsing helper, all without making network calls.

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRYPOINT="$DIR/reader-entrypoint.sh"

fail() {
    echo "FAIL: $1" >&2
    exit 1
}

# --- Missing URL env var ---
(
    unset URL
    if output=$("$ENTRYPOINT" 2>&1); then
        fail "entrypoint with no URL should exit non-zero"
    fi
    if [[ "$output" != *"URL"* ]]; then
        fail "entrypoint missing URL: expected error to mention URL, got: $output"
    fi
)

# --- URL set but no ANTHROPIC_API_KEY (claude_code is default) ---
(
    export URL="https://example.com/article"
    unset ANTHROPIC_API_KEY HARNESS OPENAI_API_KEY
    if output=$("$ENTRYPOINT" 2>&1); then
        fail "entrypoint with claude_code harness and no key should exit non-zero"
    fi
    if [[ "$output" != *"ANTHROPIC_API_KEY"* ]]; then
        fail "entrypoint claude_code missing key: expected error to mention ANTHROPIC_API_KEY, got: $output"
    fi
)

# --- URL + HARNESS=codex but no OPENAI_API_KEY ---
(
    export URL="https://example.com/article"
    export HARNESS=codex
    unset OPENAI_API_KEY ANTHROPIC_API_KEY
    if output=$("$ENTRYPOINT" 2>&1); then
        fail "entrypoint with codex harness and no key should exit non-zero"
    fi
    if [[ "$output" != *"OPENAI_API_KEY"* ]]; then
        fail "entrypoint codex missing key: expected error to mention OPENAI_API_KEY, got: $output"
    fi
)

# --- extract_reading_json: fenced JSON block in transcript text ---
(
    if [ ! -f "$DIR/reader_helpers.sh" ]; then
        fail "reader_helpers.sh is missing; extract_reading_json should live there"
    fi
    # shellcheck disable=SC1091
    source "$DIR/reader_helpers.sh"

    if ! declare -f extract_reading_json >/dev/null; then
        fail "extract_reading_json function is not defined in reader_helpers.sh"
    fi

    transcript=$'some preamble\n```json\n{\n  "url": "https://example.com/a",\n  "title": "Hi"\n}\n```\ntrailer text'
    out=$(extract_reading_json "$transcript")
    if ! printf '%s' "$out" | jq -e '.url == "https://example.com/a" and .title == "Hi"' >/dev/null; then
        fail "extract_reading_json fenced: expected url+title, got: $out"
    fi

    # No fences: balanced braces only.
    transcript2=$'preamble\n{\n  "url": "https://ex.com/b",\n  "title": "Bare"\n}\ntrailer'
    out2=$(extract_reading_json "$transcript2")
    if ! printf '%s' "$out2" | jq -e '.url == "https://ex.com/b" and .title == "Bare"' >/dev/null; then
        fail "extract_reading_json bare braces: expected url+title, got: $out2"
    fi

    # Malformed: should fail (exit non-zero).
    if extract_reading_json "no json here at all" >/dev/null 2>&1; then
        fail "extract_reading_json should fail when no JSON object present"
    fi
)

echo "ok"
