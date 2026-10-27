#!/bin/sh
# Deploys main to the VM from the laptop, at the repo root:
#
#   sh deploy/release.sh
#
# Pulls, rebuilds, waits until the service answers on its admin port, then
# runs this release's one-off steps and prints the storage figures. Every
# step is safe to run twice. A release with other one-off steps edits the
# block marked below.
set -eu

# The script goes as an argument, not on stdin: docker compose exec reads
# stdin, and would swallow the rest of a script fed that way.
remote=$(cat <<'REMOTE'
set -eu
cd /opt/transitlateagain
C="docker compose --env-file .env -f deploy/docker-compose.yml -f deploy/docker-compose.vm.yml"

git pull --ff-only
$C up -d --build

# Migrations run before the service listens, so the admin port answering
# means they are done. Five minutes, then the logs say why not.
i=0
until curl -sf -o /dev/null http://127.0.0.1:8081/v1/admin/stats; do
  i=$((i + 1))
  if [ "$i" -gt 60 ]; then
    $C logs --tail 40 transitlateagain
    exit 1
  fi
  sleep 5
done
echo "== up at $(git log --oneline -1)"

# --- One-off steps for this release (2026-10-27) ---
echo "== backfill stop_visits"
$C exec -T postgres psql -U headway -v ON_ERROR_STOP=1 -f - < deploy/backfill-stop-visits.sql
# ---

echo "== storage"
$C exec -T postgres psql -U headway -c "
  SELECT c.relname, pg_size_pretty(pg_total_relation_size(c.oid)) AS size, c.reltuples::bigint AS rows
  FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
  WHERE i.inhparent IN ('observations'::regclass, 'stop_visits'::regclass)
  ORDER BY 1" -c "
  SELECT relname, pg_size_pretty(pg_total_relation_size(oid)) AS size, reltuples::bigint AS rows
  FROM pg_class WHERE relname IN ('otp_stop_hourly', 'otp_route_hourly', 'trip_services', 'service_alerts')
  ORDER BY 1" -c "
  SELECT pg_size_pretty(pg_database_size(current_database())) AS database"
df -h / | tail -1
curl -s http://127.0.0.1:8081/v1/admin/stats
REMOTE
)
ssh root@119.42.55.16 "$remote"
