#!/bin/sh
# shellcheck shell=busybox
# Idempotent Zitadel bootstrap for Compliance OS. Safe to re-run; it converges to:
#   - project "compliance" with the fixed id $ZITADEL_PROJECT_ID (projectRoleAssertion=true),
#     so api-go's ZITADEL_AUDIENCE can be static configuration;
#   - OIDC app "web" (auth code + refresh, JWT access tokens, client_secret_basic, dev mode);
#   - one org per firm (Firm = Zitadel org) with its user, password Password1!, email verified,
#     no forced password change, and a grant of the project;
#   - one `firms` row per org in the compliance database;
# and then writes ZITADEL_ISSUER, ZITADEL_PROJECT_ID, ZITADEL_CLIENT_ID and
# ZITADEL_CLIENT_SECRET to $ENV_FILE, plus a best-effort copy to $HOST_ENV_FILE.
#
# Env:
#   ZITADEL_URL          issuer / API base (default http://zitadel.localhost:8085)
#   ZITADEL_CONNECT_TO   optional curl --connect-to HOST1:PORT1:HOST2:PORT2
#   ZITADEL_PAT_FILE     PAT of the FirstInstance machine user (default /machinekey/admin.pat)
#   ZITADEL_PROJECT_ID   required, fixed project id
#   SEED_USER_PASSWORD   password of the seeded firm users (default Password1!, dev only)
#   WEB_URL              web origin for redirect URIs (default http://localhost:3000)
#   DATABASE_OWNER_URL   required, owner connection to the compliance database
#   ENV_FILE             generated env file (default /generated/.env.generated)
#   HOST_ENV_FILE        optional second copy (compose: the host's deploy/compose/.env.generated)
set -euo pipefail

ZITADEL_URL=${ZITADEL_URL:-http://zitadel.localhost:8085}
PAT_FILE=${ZITADEL_PAT_FILE:-/machinekey/admin.pat}
PROJECT_ID=${ZITADEL_PROJECT_ID:?ZITADEL_PROJECT_ID is required}
WEB_URL=${WEB_URL:-http://localhost:3000}
DB_URL=${DATABASE_OWNER_URL:?DATABASE_OWNER_URL is required}
ENV_FILE=${ENV_FILE:-/generated/.env.generated}
HOST_ENV_FILE=${HOST_ENV_FILE:-}
DEV_PASSWORD=${SEED_USER_PASSWORD:-Password1!}

log() { echo "bootstrap: $*" >&2; }
die() { log "ERROR: $*"; exit 1; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# curl resolves every *.localhost name to loopback by itself (RFC 6761), so inside a container
# network it must be told where Zitadel really is. The URL and the Host header stay
# $ZITADEL_URL: Zitadel picks the instance by Host and stamps `iss` from it.
CONNECT_TO=
if [ -n "${ZITADEL_CONNECT_TO:-}" ]; then CONNECT_TO="--connect-to $ZITADEL_CONNECT_TO"; fi

# zapi METHOD PATH [JSON]: prints the response body. Any non-2xx logs the body and fails.
zapi() {
  local method="$1" path="$2"
  shift 2
  if [ $# -ge 1 ]; then set -- -H 'Content-Type: application/json' --data "$1"; fi
  # shellcheck disable=SC2086
  if ! curl -sS --fail-with-body $CONNECT_TO -o "$TMP/zapi" -X "$method" \
      -H "Authorization: Bearer $PAT" -H 'Accept: application/json' "$@" "$ZITADEL_URL$path"; then
    log "$method $path failed: $(cat "$TMP/zapi" 2>/dev/null)"
    return 1
  fi
  cat "$TMP/zapi"
}

# zcode METHOD PATH [JSON]: prints the HTTP status only; the body is left in $TMP/resp.
zcode() {
  local method="$1" path="$2"
  shift 2
  if [ $# -ge 1 ]; then set -- -H 'Content-Type: application/json' --data "$1"; fi
  # shellcheck disable=SC2086
  curl -sS $CONNECT_TO -o "$TMP/resp" -w '%{http_code}' -X "$method" \
    -H "Authorization: Bearer $PAT" -H 'Accept: application/json' "$@" "$ZITADEL_URL$path"
}

# The secret is only returned when an app is created (or regenerated): reuse the one already
# written for this client id, if any.
existing_secret() {
  for f in "$ENV_FILE" ${HOST_ENV_FILE:+"$HOST_ENV_FILE"}; do
    [ -r "$f" ] || continue
    if [ "$(sed -n 's/^ZITADEL_CLIENT_ID=//p' "$f")" = "$1" ]; then
      sed -n 's/^ZITADEL_CLIENT_SECRET=//p' "$f"
      return 0
    fi
  done
}

# write_env PATH MODE: atomic write (temp file in the same directory, then rename).
write_env() {
  tmp="$(dirname "$1")/.env.generated.$$"
  {
    echo "# Written by deploy/compose/zitadel/bootstrap.sh. Dev-only credentials; do not commit."
    echo "ZITADEL_ISSUER=$ISSUER"
    echo "ZITADEL_PROJECT_ID=$PROJECT_ID"
    echo "ZITADEL_CLIENT_ID=$CLIENT_ID"
    echo "ZITADEL_CLIENT_SECRET=$CLIENT_SECRET"
  } >"$tmp" || return 1
  chmod "$2" "$tmp" || { rm -f "$tmp"; return 1; }
  mv -f "$tmp" "$1" || { rm -f "$tmp"; return 1; }
}

# --- 0. Wait for the PAT and for Zitadel to accept it --------------------------------------
i=0
until [ -s "$PAT_FILE" ]; do
  i=$((i + 1))
  [ "$i" -le 60 ] || die "no PAT at $PAT_FILE after 120s"
  sleep 2
done
PAT=$(cat "$PAT_FILE")

i=0
until [ "$(zcode GET /auth/v1/users/me || true)" = 200 ]; do
  i=$((i + 1))
  [ "$i" -le 60 ] || die "Zitadel does not accept the bootstrap PAT: $(cat "$TMP/resp" 2>/dev/null)"
  sleep 2
done

ISSUER=$(zapi GET /.well-known/openid-configuration | jq -er .issuer)
[ "$ISSUER" = "$ZITADEL_URL" ] || die "Zitadel issuer is $ISSUER, expected $ZITADEL_URL"

# --- 1. Project "compliance" with a fixed id ----------------------------------------------
code=$(zcode GET "/management/v1/projects/$PROJECT_ID")
case $code in
  200)
    log "project compliance exists ($PROJECT_ID)"
    ;;
  404)
    platform_org=$(zapi GET /management/v1/orgs/me | jq -er .org.id)
    # v4.3.0 has no REST route for the v2beta ProjectService (POST /v2beta/projects is 404);
    # its Connect route is the only API that takes a caller-chosen project id.
    zapi POST /zitadel.project.v2beta.ProjectService/CreateProject "$(jq -nc --arg org "$platform_org" --arg id "$PROJECT_ID" \
      '{organizationId: $org, id: $id, name: "compliance", projectRoleAssertion: true}')" >/dev/null
    log "created project compliance ($PROJECT_ID)"
    ;;
  *)
    die "GET project $PROJECT_ID: HTTP $code: $(cat "$TMP/resp")"
    ;;
esac

# --- 2. OIDC app "web" --------------------------------------------------------------------
apps=$(zapi POST "/management/v1/projects/$PROJECT_ID/apps/_search" \
  '{"queries":[{"nameQuery":{"name":"web","method":"TEXT_QUERY_METHOD_EQUALS"}}]}')
APP_ID=$(printf '%s' "$apps" | jq -r '.result[0].id // empty')
if [ -z "$APP_ID" ]; then
  created=$(zapi POST "/management/v1/projects/$PROJECT_ID/apps/oidc" "$(jq -nc --arg web "$WEB_URL" '{
    name: "web",
    redirectUris: [($web + "/api/auth/callback/zitadel")],
    postLogoutRedirectUris: [$web],
    responseTypes: ["OIDC_RESPONSE_TYPE_CODE"],
    grantTypes: ["OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"],
    appType: "OIDC_APP_TYPE_WEB",
    authMethodType: "OIDC_AUTH_METHOD_TYPE_BASIC",
    accessTokenType: "OIDC_TOKEN_TYPE_JWT",
    devMode: true
  }')")
  CLIENT_ID=$(printf '%s' "$created" | jq -er .clientId)
  CLIENT_SECRET=$(printf '%s' "$created" | jq -er .clientSecret)
  log "created OIDC app web (client $CLIENT_ID)"
else
  CLIENT_ID=$(printf '%s' "$apps" | jq -er '.result[0].oidcConfig.clientId')
  CLIENT_SECRET=$(existing_secret "$CLIENT_ID")
  if [ -n "$CLIENT_SECRET" ]; then
    log "OIDC app web exists (client $CLIENT_ID); reusing its secret"
  else
    CLIENT_SECRET=$(zapi POST "/management/v1/projects/$PROJECT_ID/apps/$APP_ID/oidc_config/_generate_client_secret" '{}' \
      | jq -er .clientSecret)
    log "OIDC app web exists (client $CLIENT_ID); no stored secret, regenerated it"
  fi
fi
case $CLIENT_ID in *[!A-Za-z0-9@._-]* | '') die "unexpected client id '$CLIENT_ID'" ;; esac
case $CLIENT_SECRET in *[!A-Za-z0-9]* | '') die "unexpected characters in the client secret" ;; esac

# --- 3. Firms: org + user + firms row + project grant ------------------------------------
# seed_firm NAME EMAIL GIVEN_NAME FAMILY_NAME
seed_firm() {
  name=$1 email=$2
  human=$(jq -nc --arg e "$email" --arg g "$3" --arg f "$4" --arg p "$DEV_PASSWORD" '{
    username: $e,
    profile: {givenName: $g, familyName: $f},
    email: {email: $e, isVerified: true},
    password: {password: $p, changeRequired: false}
  }')

  org=$(zapi POST /v2/organizations/_search "$(jq -nc --arg n "$name" \
    '{queries: [{nameQuery: {name: $n, method: "TEXT_QUERY_METHOD_EQUALS"}}]}')" | jq -r '.result[0].id // empty')
  if [ -z "$org" ]; then
    org=$(zapi POST /v2/organizations "$(jq -nc --arg n "$name" --argjson h "$human" \
      '{name: $n, admins: [{human: $h}]}')" | jq -er .organizationId)
    log "created org $name ($org) with user $email"
  else
    users=$(zapi POST /v2/users "$(jq -nc --arg e "$email" \
      '{queries: [{loginNameQuery: {loginName: $e, method: "TEXT_QUERY_METHOD_EQUALS"}}]}')")
    if [ "$(printf '%s' "$users" | jq '.result // [] | length')" = 0 ]; then
      zapi POST /v2/users/human "$(printf '%s' "$human" | jq -c --arg o "$org" '. + {organization: {orgId: $o}}')" >/dev/null
      log "org $name exists ($org); created missing user $email"
    else
      log "org $name exists ($org) with user $email"
    fi
  fi

  psql "$DB_URL" -v ON_ERROR_STOP=1 -q -v org="$org" -v name="$name" <<'SQL'
INSERT INTO firms (zitadel_org_id, name) VALUES (:'org', :'name') ON CONFLICT (zitadel_org_id) DO NOTHING;
SQL

  code=$(zcode POST "/management/v1/projects/$PROJECT_ID/grants" "$(jq -nc --arg o "$org" '{grantedOrgId: $o}')")
  case $code in
    2??) log "granted project compliance to $name" ;;
    409) log "project compliance already granted to $name" ;;
    *) die "grant project to $name: HTTP $code: $(cat "$TMP/resp")" ;;
  esac
}

seed_firm "Demo Firm A" a@firm-a.test Firm-A User
seed_firm "Demo Firm B" b@firm-b.test Firm-B User

# --- 4. Generated env ---------------------------------------------------------------------
# Readable by the web container (another non-root uid) through the shared volume.
write_env "$ENV_FILE" 0644 || die "cannot write $ENV_FILE"
log "wrote $ENV_FILE"
if [ -n "$HOST_ENV_FILE" ]; then
  if write_env "$HOST_ENV_FILE" 0600 2>/dev/null; then
    log "wrote $HOST_ENV_FILE"
  else
    log "WARN: cannot write $HOST_ENV_FILE as uid $(id -u); run make up with HOST_UID=\$(id -u) HOST_GID=\$(id -g) (the stack itself does not need the host copy)"
  fi
fi
log "done"
