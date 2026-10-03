#!/usr/bin/env bash
# AC-E2 chaos run (Phase 1 Task 29): the host's single entry point for TestBatchChaos1000.
#   - starts a throwaway Postgres 17.6 (p1-t29-pg, port 45291) and applies
#     deploy/compose/postgres/init.sql, and a throwaway MinIO (p1-t29-minio, port 45292);
#   - syncs ai-py's environment once (uv sync --frozen), so the worker restart is quick;
#   - runs go test -tags chaos -run TestBatchChaos1000 -timeout 15m ./internal/chaos/... (api-go
#     in-process, embedded JetStream, ai-py as a real `uv run python -m ai` process);
#   - removes both containers and kills any worker process group left behind, even on failure.
# No docker compose, no k3d (PARALLEL-CONTRACTS rule 10): run it only when the host says the stack is
# free. Knobs: CHAOS_PG_PORT, CHAOS_MINIO_PORT, CHAOS_BIND (publish address, default 127.0.0.1),
# CHAOS_HOST (address the test dials, default 127.0.0.1), CHAOS_AI_FAKE_LATENCY_MS,
# CHAOS_AI_FAKE_SCENARIO. Logs (go test output, worker logs) stay in the printed directory.
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$(pwd)

PG_NAME=p1-t29-pg
MINIO_NAME=p1-t29-minio
PG_IMAGE=postgres:17.6
MINIO_IMAGE=bitnamilegacy/minio:2025.7.23-debian-12-r5 # deploy/compose/compose.yaml:74
PG_PORT=${CHAOS_PG_PORT:-45291}
MINIO_PORT=${CHAOS_MINIO_PORT:-45292}
BIND=${CHAOS_BIND:-127.0.0.1}
HOST=${CHAOS_HOST:-127.0.0.1}
LOG_DIR=$(mktemp -d "${TMPDIR:-/tmp}/p1-t29-chaos.XXXXXX")

for tool in docker uv go curl; do
  command -v "$tool" >/dev/null || { echo "chaos: $tool not found" >&2; exit 1; }
done

cleanup() {
  local rc=$?
  shopt -s nullglob
  # A worker whose go test died before its cleanup (timeout panic, ^C) is still running.
  for f in "$LOG_DIR"/*.pgid; do
    kill -KILL -- "-$(cat "$f")" 2>/dev/null || true
  done
  docker rm -f "$PG_NAME" "$MINIO_NAME" >/dev/null 2>&1 || true
  echo "chaos: containers removed; logs in $LOG_DIR (exit $rc)"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

wait_for() { # <what> <seconds> <command...>
  local what=$1 secs=$2
  shift 2
  for ((i = 0; i < secs; i++)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo "chaos: $what not ready after ${secs}s" >&2
  return 1
}

docker rm -f "$PG_NAME" "$MINIO_NAME" >/dev/null 2>&1 || true # leftovers of an aborted run
docker run -d --rm --name "$PG_NAME" -p "$BIND:$PG_PORT:5432" -e POSTGRES_PASSWORD=pg "$PG_IMAGE" >/dev/null
docker run -d --rm --name "$MINIO_NAME" -p "$BIND:$MINIO_PORT:9000" \
  -e MINIO_ROOT_USER=minio -e MINIO_ROOT_PASSWORD=minio_dev_pw "$MINIO_IMAGE" >/dev/null

# TCP, not the socket: the entrypoint's first-init server listens on the socket only.
wait_for postgres 90 docker exec "$PG_NAME" pg_isready -h 127.0.0.1 -U postgres
# Piped, not bind-mounted: on this host docker may be Docker Desktop's docker.exe, which cannot
# mount a WSL path.
docker exec -i "$PG_NAME" psql -h 127.0.0.1 -U postgres -v ON_ERROR_STOP=1 -q -f - \
  <deploy/compose/postgres/init.sql
wait_for minio 90 curl -sf "http://$HOST:$MINIO_PORT/minio/health/ready"

(cd services/ai-py && uv sync --frozen)

echo "chaos: running TestBatchChaos1000 (logs in $LOG_DIR)"
cd services/api-go
TEST_OWNER_URL="postgres://compliance_owner:owner_dev_pw@$HOST:$PG_PORT/compliance?sslmode=disable" \
TEST_APP_URL="postgres://compliance_app:app_dev_pw@$HOST:$PG_PORT/compliance?sslmode=disable" \
CHAOS_S3_ENDPOINT="$HOST:$MINIO_PORT" CHAOS_S3_ACCESS_KEY=minio CHAOS_S3_SECRET_KEY=minio_dev_pw \
CHAOS_AIPY_DIR="$ROOT/services/ai-py" CHAOS_LOG_DIR="$LOG_DIR" \
  go test -tags chaos -run 'TestBatchChaos1000$' -count=1 -v -timeout 15m ./internal/chaos/... 2>&1 |
  tee "$LOG_DIR/go-test.log"
