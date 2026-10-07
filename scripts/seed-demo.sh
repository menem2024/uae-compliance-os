#!/usr/bin/env bash
# Fills a running stack with synthetic UAE demo data so the app does not look empty.
#
#   bash scripts/seed-demo.sh                 # against http://localhost:3000, dev users, dev password
#   BASE_URL=https://demo.example SEED_PASSWORD=... bash scripts/seed-demo.sh
#   bash scripts/seed-demo.sh --dry-run       # print the plan, touch nothing, no login
#   bash scripts/seed-demo.sh --dump DIR      # also write the generated invoice JSON files to DIR
#
# Environment (all optional):
#   BASE_URL          web app origin, default http://localhost:3000
#   SEED_EMAIL        firm A user, default a@firm-a.test
#   SEED_PASSWORD     firm A password; falls back to SEED_USER_PASSWORD, then the local dev password
#   SEED_EMAIL_B      firm B user, default b@firm-b.test
#   SEED_PASSWORD_B   firm B password, defaults to the firm A password
#   SEED_FIRM_B=0     skip firm B (the isolation proof)
#   SEED_BRAND=0      do not set a brand colour on a firm that has none
#
# It signs in through the app's normal Zitadel login in a headless browser and calls the app's own
# /api routes, so every step goes through api-go, the validator and the audit log. It is idempotent
# (check before create, deterministic invoice numbers) and never deletes anything.
set -euo pipefail
cd "$(dirname "$0")/../e2e"

command -v node >/dev/null 2>&1 || { echo "node is required (Node 22.18+ runs the TypeScript helper directly)." >&2; exit 1; }
node -e 'const [a,b]=process.versions.node.split(".").map(Number); process.exit(a>22||(a===22&&b>=18)?0:1)' \
  || { echo "Node 22.18+ is required (found $(node --version))." >&2; exit 1; }

if [ ! -d node_modules/@playwright/test ]; then
  echo "==> installing e2e dependencies (npm ci)"
  npm ci --no-audit --no-fund
fi

dry=0
for a in "$@"; do case "$a" in --dry-run|--dump) dry=1 ;; esac; done

if [ "$dry" = 0 ]; then
  base="${BASE_URL:-http://localhost:3000}"
  code=$(curl -s -o /dev/null -m 10 -w '%{http_code}' "${base%/}/en" || true)
  case "$code" in
    2*|3*) ;;
    *) echo "The app at $base is not reachable (HTTP $code). Start the stack first (bash scripts/demo.sh)." >&2; exit 1 ;;
  esac
  if ! ls "${PLAYWRIGHT_BROWSERS_PATH:-$HOME/.cache/ms-playwright}"/chromium-* >/dev/null 2>&1; then
    echo "==> installing the Playwright Chromium browser"
    npx playwright install chromium
  fi
fi

exec node --no-warnings seed/seed-demo.ts "$@"
