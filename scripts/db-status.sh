#!/usr/bin/env bash
set -euo pipefail

DB="${BACKFLOW_DATABASE_URL:?BACKFLOW_DATABASE_URL is required}"

echo "=== Tasks ==="
psql "$DB" -c "SELECT * FROM tasks ORDER BY created_at DESC;"

echo ""
echo "=== Task Summary ==="
psql "$DB" -c "SELECT status, count(*) as count FROM tasks GROUP BY status;"

echo ""
echo "=== Instances ==="
psql "$DB" -c "
    SELECT instance_id, instance_type, status, private_ip,
           running_containers || '/' || max_containers as containers,
           created_at, updated_at
    FROM instances ORDER BY created_at DESC;
"

echo ""
echo "=== Instance Summary ==="
psql "$DB" -c "SELECT status, count(*) as count FROM instances GROUP BY status;"
