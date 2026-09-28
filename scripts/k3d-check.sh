#!/usr/bin/env bash
# Live k3d/Helm deploy check for Compliance OS (Story 10, AC4 k8s half).
# Run after: make k3d-up images-k3d helm-install
set -euo pipefail

NS=compliance

kubectl -n "$NS" wait --for=condition=Ready pod -l app.kubernetes.io/part-of=compliance --timeout=600s
kubectl -n "$NS" wait --for=condition=complete job/migrate --timeout=300s
kubectl -n "$NS" wait --for=condition=complete job/zitadel-bootstrap --timeout=300s

kubectl -n "$NS" port-forward svc/api-go 18080:8080 >/dev/null 2>&1 &
PF=$!
trap 'kill "$PF" 2>/dev/null || true' EXIT
sleep 3

curl -sf http://127.0.0.1:18080/readyz && echo " OK"
