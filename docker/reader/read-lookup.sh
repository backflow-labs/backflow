#!/usr/bin/env bash
set -euo pipefail

# read-lookup.sh — look up an existing reading by exact URL match.
# Usage: read-lookup.sh <url>
# Output: JSON array (empty = no match, single element = duplicate).

if [ -z "${SUPABASE_URL:-}" ]; then
    echo "read-lookup: SUPABASE_URL is not set" >&2
    exit 1
fi

if [ -z "${SUPABASE_READER_KEY:-}" ]; then
    echo "read-lookup: SUPABASE_READER_KEY is not set" >&2
    exit 1
fi

if [ $# -lt 1 ] || [ -z "$1" ]; then
    echo "read-lookup: URL argument is required" >&2
    exit 1
fi

URL="$1"
ENCODED=$(printf '%s' "$URL" | jq -sRr @uri)

RESPONSE=$(curl -fsS \
    -H "apikey: ${SUPABASE_READER_KEY}" \
    -H "Authorization: Bearer ${SUPABASE_READER_KEY}" \
    "${SUPABASE_URL}/rest/v1/readings?url=eq.${ENCODED}&select=id,url,title,tldr") || {
    echo "read-lookup: Supabase REST request failed" >&2
    exit 1
}

if ! printf '%s' "$RESPONSE" | jq -e 'type == "array"' >/dev/null 2>&1; then
    echo "read-lookup: unexpected response from Supabase: $RESPONSE" >&2
    exit 1
fi

printf '%s\n' "$RESPONSE" | jq -c .
