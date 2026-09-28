#!/usr/bin/env bash
# Asserts the compose stack started by `make up` is healthy end to end:
#   - every long-running service reports Docker health "healthy";
#   - the one-shot jobs (migrate, zitadel-bootstrap) exited 0;
#   - api-go /readyz returns 200 (via its own `healthcheck` subcommand);
#   - the bootstrap seeded both firms and wrote deploy/compose/.env.generated;
#   - api-go's audience and web's OIDC client match what the bootstrap wrote;
#   - Zitadel's issuer is the same URL inside the network and on the host;
#   - no process in any compose container runs as root.
set -euo pipefail
cd "$(dirname "$0")/.."

F=deploy/compose/compose.yaml
ENV_FILE=deploy/compose/.env.generated
ISSUER=http://zitadel.localhost:8085

dc() { docker compose -f "$F" "$@"; }
fail() { echo "FAIL $*" >&2; exit 1; }

for s in postgres nats valkey minio otel-lgtm zitadel validator-rs api-go ai-py web; do
  state=$(dc ps --format '{{.Health}}' "$s" 2>/dev/null || true)
  [[ "$state" == "healthy" ]] || fail "$s is '$state'"
done

for job in migrate zitadel-bootstrap; do
  state=$(dc ps -a --format '{{.State}} {{.ExitCode}}' "$job" 2>/dev/null || true)
  [[ "$state" == "exited 0" ]] || fail "$job is '$state' (want 'exited 0')"
done

dc exec -T api-go /api healthcheck || fail "api-go /readyz is not 200"

firms=$(dc exec -T postgres psql -U postgres -d compliance -tAc \
  "select string_agg(name, ',' order by name) from firms")
[[ "$firms" == "Demo Firm A,Demo Firm B" ]] || fail "firms are '$firms'"

[[ -f "$ENV_FILE" ]] || fail "$ENV_FILE is missing (if the bootstrap logged a WARN about /out, run make up with HOST_UID=\$(id -u) HOST_GID=\$(id -g))"
for k in ZITADEL_ISSUER ZITADEL_PROJECT_ID ZITADEL_CLIENT_ID ZITADEL_CLIENT_SECRET; do
  grep -q "^$k=." "$ENV_FILE" || fail "$ENV_FILE has no $k"
done
grep -qx "ZITADEL_ISSUER=$ISSUER" "$ENV_FILE" || fail "$ENV_FILE issuer is not $ISSUER"
project_id=$(sed -n 's/^ZITADEL_PROJECT_ID=//p' "$ENV_FILE")
client_id=$(sed -n 's/^ZITADEL_CLIENT_ID=//p' "$ENV_FILE")

api_env=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$(dc ps -q api-go)")
grep -qx "ZITADEL_AUDIENCE=$project_id" <<<"$api_env" || fail "api-go ZITADEL_AUDIENCE is not the project id $project_id"

# The running web process (PID 1) must have the client the bootstrap wrote.
web_env=$(dc exec -T web sh -c 'tr "\0" "\n" </proc/1/environ')
grep -qx "ZITADEL_CLIENT_ID=$client_id" <<<"$web_env" || fail "web ZITADEL_CLIENT_ID does not match $ENV_FILE"
grep -q '^ZITADEL_CLIENT_SECRET=.' <<<"$web_env" || fail "web has no ZITADEL_CLIENT_SECRET"

# One Zitadel hostname: the same issuer from inside the network and from the host.
dc exec -T web node -e "fetch('$ISSUER/.well-known/openid-configuration').then(r=>r.json()).then(j=>process.exit(j.issuer==='$ISSUER'?0:1)).catch(()=>process.exit(1))" \
  || fail "web cannot read the $ISSUER discovery document"
host_issuer=$(curl -sf "$ISSUER/.well-known/openid-configuration" | sed -n 's/.*"issuer":"\([^"]*\)".*/\1/p')
[[ "$host_issuer" == "$ISSUER" ]] || fail "host sees issuer '$host_issuer'"

for id in $(dc ps -aq); do
  name=$(docker inspect -f '{{.Name}}' "$id")
  user=$(docker inspect -f '{{.Config.User}}' "$id")
  [[ -n "$user" && "${user%%:*}" != 0 && "${user%%:*}" != root ]] || fail "$name has no non-root user ('$user')"
done
for id in $(dc ps -q); do
  name=$(docker inspect -f '{{.Name}}' "$id")
  uids=$(docker top "$id" -eo pid,uid | awk 'NR>1 {print $2}' | sort -u | tr '\n' ' ')
  [[ -n "$uids" ]] || fail "$name: no processes"
  for u in $uids; do [[ "$u" != 0 ]] || fail "$name has a process running as root"; done
done

echo OK
