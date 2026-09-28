#!/usr/bin/env bash
# Zitadel bootstrap for the k3d/Helm deploy (mirrors deploy/compose/zitadel/bootstrap.sh
# from Story 6, adapted for in-cluster kubectl instead of an .env.generated file).
# Idempotent: safe to re-run on `helm upgrade`.
#
# Env (set by the zitadel-bootstrap-job Deployment):
#   ZITADEL_URL            base URL, e.g. http://zitadel.compliance.localhost:8081
#   WEB_URL                web origin, e.g. http://compliance.localhost:8081
#   NAMESPACE              k8s namespace to write the Secret into
#   PAT_FILE               path to the mounted admin PAT (default /secrets/pat)
#   DATABASE_OWNER_URL     compliance_owner Postgres DSN, for the firms upsert
set -euo pipefail

ZITADEL_URL="${ZITADEL_URL:?ZITADEL_URL is required}"
WEB_URL="${WEB_URL:?WEB_URL is required}"
NAMESPACE="${NAMESPACE:?NAMESPACE is required}"
PAT_FILE="${PAT_FILE:-/secrets/pat}"
DATABASE_OWNER_URL="${DATABASE_OWNER_URL:?DATABASE_OWNER_URL is required}"

PAT="$(cat "$PAT_FILE")"
AUTH_HEADER="Authorization: Bearer ${PAT}"

curl_json() {
  # curl_json METHOD PATH [BODY]
  local method="$1" path="$2" body="${3:-}"
  if [ -n "$body" ]; then
    curl --fail-with-body -sS -X "$method" "${ZITADEL_URL}${path}" \
      -H "$AUTH_HEADER" -H "Content-Type: application/json" -d "$body"
  else
    curl --fail-with-body -sS -X "$method" "${ZITADEL_URL}${path}" \
      -H "$AUTH_HEADER" -H "Content-Type: application/json"
  fi
}

echo "waiting for ${ZITADEL_URL}/debug/ready ..."
for _ in $(seq 1 60); do
  if curl -sf "${ZITADEL_URL}/debug/ready" >/dev/null 2>&1; then
    break
  fi
  sleep 5
done

# --- 1. Project "compliance" (find-or-create) ---------------------------------
PROJECT_ID="$(curl_json POST /management/v1/projects/_search '{"query":{"offset":"0","limit":100},"queries":[{"nameQuery":{"name":"compliance","method":"TEXT_QUERY_METHOD_EQUALS"}}]}' \
  | jq -r '.result[0].id // empty')"
if [ -z "$PROJECT_ID" ]; then
  PROJECT_ID="$(curl_json POST /management/v1/projects '{"name":"compliance","projectRoleAssertion":true}' | jq -r '.id')"
fi
echo "project id: ${PROJECT_ID}"

# --- 2. OIDC app "web" (find-or-create, secret only known at creation) --------
APP_ID="$(curl_json POST "/management/v1/projects/${PROJECT_ID}/apps/_search" '{"query":{"offset":"0","limit":100}}' \
  | jq -r '.result[]? | select(.name=="web") | .id' | head -n1)"

CLIENT_ID=""
CLIENT_SECRET=""
EXISTING_SECRET="$(kubectl --namespace "$NAMESPACE" get secret zitadel-oidc -o jsonpath='{.data.ZITADEL_CLIENT_SECRET}' 2>/dev/null | base64 -d || true)"

if [ -z "$APP_ID" ]; then
  RESP="$(curl_json POST "/management/v1/projects/${PROJECT_ID}/apps/oidc" '{
    "name": "web",
    "redirectUris": ["'"${WEB_URL}"'/api/auth/callback/zitadel"],
    "postLogoutRedirectUris": ["'"${WEB_URL}"'"],
    "responseTypes": ["OIDC_RESPONSE_TYPE_CODE"],
    "grantTypes": ["OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"],
    "appType": "OIDC_APP_TYPE_WEB",
    "authMethodType": "OIDC_AUTH_METHOD_TYPE_BASIC",
    "accessTokenType": "OIDC_TOKEN_TYPE_JWT",
    "devMode": true
  }')"
  APP_ID="$(echo "$RESP" | jq -r '.appId')"
  CLIENT_ID="$(echo "$RESP" | jq -r '.clientId')"
  CLIENT_SECRET="$(echo "$RESP" | jq -r '.clientSecret')"
else
  CLIENT_ID="$(curl_json GET "/management/v1/projects/${PROJECT_ID}/apps/${APP_ID}/oidc" | jq -r '.app.clientId // .app.oidcConfig.clientId')"
  if [ -n "$EXISTING_SECRET" ]; then
    CLIENT_SECRET="$EXISTING_SECRET"
  else
    CLIENT_SECRET="$(curl_json POST "/management/v1/projects/${PROJECT_ID}/apps/${APP_ID}/oidc_config/_generate_client_secret" '{}' | jq -r '.clientSecret')"
  fi
fi
echo "app id: ${APP_ID}, client id: ${CLIENT_ID}"

# --- 3. Demo firms: org + admin user + Postgres row ----------------------------
setup_firm() {
  local org_name="$1" admin_email="$2"
  local org_id
  org_id="$(curl_json POST /admin/v1/orgs/_search '{"query":{"offset":"0","limit":100},"queries":[{"nameQuery":{"name":"'"${org_name}"'","method":"TEXT_QUERY_METHOD_EQUALS"}}]}' \
    | jq -r '.result[0].id // empty')"
  if [ -z "$org_id" ]; then
    RESP="$(curl_json POST /v2/organizations '{
      "name": "'"${org_name}"'",
      "admins": [{
        "human": {
          "email": {"email": "'"${admin_email}"'", "isVerified": true},
          "profile": {"givenName": "Demo", "familyName": "Admin"},
          "password": {"password": "Password1!", "changeRequired": false}
        }
      }]
    }')"
    org_id="$(echo "$RESP" | jq -r '.organizationId')"
  fi
  echo "org ${org_name}: ${org_id}"

  # Idempotent upsert into the compliance schema (Task 3's firms table).
  psql "$DATABASE_OWNER_URL" -v ON_ERROR_STOP=1 -c \
    "INSERT INTO firms(zitadel_org_id, name) VALUES ('${org_id}', '${org_name}') ON CONFLICT (zitadel_org_id) DO NOTHING;"

  # Grant the project to the org so its users can log into the web app.
  curl_json POST "/management/v1/projects/${PROJECT_ID}/grants" '{"projectId":"'"${PROJECT_ID}"'","grantedOrgId":"'"${org_id}"'"}' >/dev/null 2>&1 || true
}

setup_firm "Demo Firm A" "a@firm-a.test"
setup_firm "Demo Firm B" "b@firm-b.test"

# --- 4. Write the OIDC client into the zitadel-oidc Secret ---------------------
kubectl --namespace "$NAMESPACE" create secret generic zitadel-oidc \
  --from-literal=ZITADEL_ISSUER="${ZITADEL_URL}" \
  --from-literal=ZITADEL_CLIENT_ID="${CLIENT_ID}" \
  --from-literal=ZITADEL_CLIENT_SECRET="${CLIENT_SECRET}" \
  --from-literal=ZITADEL_AUDIENCE="${PROJECT_ID}" \
  --dry-run=client --output=yaml | kubectl apply --namespace "$NAMESPACE" -f -

echo "zitadel bootstrap done"
