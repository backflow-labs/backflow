---
name: goose-migration
description: Create and edit Backflow goose migrations in migrations/.
---

# Goose Migration Skill

Use this when creating or updating a Backflow database migration.

## Workflow

1. Inspect `migrations/` and find the highest existing numeric prefix in `NNN_*.sql`.
2. If the directory is empty, start with `001`.
3. Ask what schema change is needed if it is not already specified.
4. Create the next file as `NNN_slug.sql` with `-- +goose Up` and `-- +goose Down` sections.
5. Keep the SQL Postgres-native and consistent with `migrations/001_initial_schema.sql`.
6. Prefer `TEXT`, `INTEGER`, `BOOLEAN`, `DOUBLE PRECISION`, `JSONB`, and `TIMESTAMPTZ` over SQLite idioms.
7. Make the migration reversible. Use `IF EXISTS` / `IF NOT EXISTS` where it helps safety.

## Output shape

- Only generate the new migration file unless the user asks for additional changes.
- Keep index names consistent with existing conventions, such as `idx_<table>_<column>`.
