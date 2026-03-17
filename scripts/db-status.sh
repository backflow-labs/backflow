#!/usr/bin/env bash
set -euo pipefail

DB_URL="${BACKFLOW_DATABASE_URL:-}"

if [ -z "$DB_URL" ]; then
    echo "BACKFLOW_DATABASE_URL is required"
    exit 1
fi

echo "=== Tasks ==="
psql "$DB_URL" -c "SELECT * FROM tasks ORDER BY created_at DESC;"

echo ""
echo "=== Task Summary ==="
psql "$DB_URL" -c "SELECT status, count(*) AS count FROM tasks GROUP BY status ORDER BY status;"

echo ""
echo "=== Instances ==="
psql "$DB_URL" -c "
    SELECT instance_id, instance_type, status, private_ip,
           running_containers::text || '/' || max_containers::text AS containers,
           created_at, updated_at
    FROM instances ORDER BY created_at DESC;
"

echo ""
echo "=== Instance Summary ==="
psql "$DB_URL" -c "SELECT status, count(*) AS count FROM instances GROUP BY status ORDER BY status;"
