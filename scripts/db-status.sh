#!/usr/bin/env bash
set -euo pipefail

DB="${BACKFLOW_DATABASE_URL:-}"

if [ -z "$DB" ]; then
    echo "BACKFLOW_DATABASE_URL is not set"
    exit 1
fi

echo "=== Tasks ==="
psql "$DB" -c "SELECT * FROM tasks ORDER BY created_at DESC;"

echo ""
echo "=== Task Summary ==="
psql "$DB" -c "SELECT status, count(*) AS count FROM tasks GROUP BY status;"

echo ""
echo "=== Instances ==="
psql "$DB" -c "
    SELECT instance_id, instance_type, status, private_ip,
           running_containers || '/' || max_containers AS containers,
           created_at, updated_at
    FROM instances ORDER BY created_at DESC;"

echo ""
echo "=== Instance Summary ==="
psql "$DB" -c "SELECT status, count(*) AS count FROM instances GROUP BY status;"
