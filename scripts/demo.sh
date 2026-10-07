#!/usr/bin/env bash
# One-command local demo: brings up the full stack with Docker Compose and prints where to look.
# Requires Docker (Docker Desktop with WSL integration on Windows). No API key is needed: the demo path
# (canonical invoice -> deterministic PINT-AE validation -> correction -> approval -> XML export -> audit)
# does not call a language model. PDF extraction by an LLM needs ANTHROPIC_API_KEY (see README).
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "Docker is not reachable. Start Docker Desktop (enable Settings > Resources > WSL integration for this distro), then re-run." >&2
  exit 1
fi

echo "==> building and starting the stack (first run builds Rust/Go/Python/Next images: several minutes)"
make up

if [ -x scripts/compose-check.sh ]; then
  echo "==> health check"
  bash scripts/compose-check.sh
fi

cat <<'MSG'

Demo is up.

  App        http://localhost:3000            (sign in: a@firm-a.test / Password1!)
  Invoices   http://localhost:3000/en/invoices   (pick a sample invoice, review the findings)
  Agents     http://localhost:3000/en/agents      (live agent activity)
  Traces     http://localhost:3001             (Grafana, one trace across web -> Go -> NATS -> Python -> Rust)
  Zitadel    http://zitadel.localhost:8085/ui/console

Stop with: make down
MSG
