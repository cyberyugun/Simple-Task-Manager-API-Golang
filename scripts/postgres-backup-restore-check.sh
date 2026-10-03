#!/usr/bin/env bash
set -euo pipefail

: "${BACKUP_DATABASE_URL:?BACKUP_DATABASE_URL is required}"
: "${RESTORE_DATABASE_URL:?RESTORE_DATABASE_URL is required}"
: "${ALLOW_RESTORE_TARGET_RESET:?ALLOW_RESTORE_TARGET_RESET must be set to true}"

if [ "$ALLOW_RESTORE_TARGET_RESET" != "true" ]; then
  echo "ALLOW_RESTORE_TARGET_RESET must be true" >&2
  exit 1
fi

if [ "$BACKUP_DATABASE_URL" = "$RESTORE_DATABASE_URL" ]; then
  echo "Source and restore database URLs must be different." >&2
  exit 1
fi

for command in pg_dump pg_restore psql; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "required command missing: $command" >&2
    exit 1
  }
done

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
chmod 700 "$tmpdir"

dump_file="$tmpdir/task-manager.dump"
pg_dump   --format=custom   --no-owner   --no-acl   --dbname="$BACKUP_DATABASE_URL"   --file="$dump_file"
chmod 600 "$dump_file"

psql "$RESTORE_DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
DROP SCHEMA IF EXISTS public CASCADE;
CREATE SCHEMA public;
SQL

pg_restore   --exit-on-error   --no-owner   --no-acl   --dbname="$RESTORE_DATABASE_URL"   "$dump_file"

source_tables="$(psql "$BACKUP_DATABASE_URL" -At -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE';")"
restore_tables="$(psql "$RESTORE_DATABASE_URL" -At -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE';")"

if [ "$source_tables" != "$restore_tables" ]; then
  echo "restored table count mismatch: source=$source_tables restored=$restore_tables" >&2
  exit 1
fi

source_migrations="$(psql "$BACKUP_DATABASE_URL" -At -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM schema_migrations;")"
restore_migrations="$(psql "$RESTORE_DATABASE_URL" -At -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM schema_migrations;")"

if [ "$source_migrations" != "$restore_migrations" ]; then
  echo "restored migration count mismatch: source=$source_migrations restored=$restore_migrations" >&2
  exit 1
fi

psql "$RESTORE_DATABASE_URL" -v ON_ERROR_STOP=1 -c "SELECT 1;" >/dev/null

echo "Backup/restore verification PASSED: tables=$restore_tables migrations=$restore_migrations"
