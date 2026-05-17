#!/usr/bin/env bash
# Run load test case TC1–TC5 on Kind via Helm overlays + in-cluster k6 Job.
# Usage: ./scripts/load-test/run-tc-kind.sh <1-5>
# Prereq: make kind-build && kind cluster running
set -euo pipefail

TC="${1:?usage: $0 <1-5>}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
HELM_RELEASE="${HELM_RELEASE:-otel-traces-test}"
HELM_NAMESPACE="${HELM_NAMESPACE:-otel}"
CHART="$ROOT/charts/otel-traces-test"
OVERLAY="$CHART/values-load-tc${TC}.yaml"

if [[ ! -f "$OVERLAY" ]]; then
  echo "missing $OVERLAY" >&2
  exit 1
fi

echo "==> TC${TC}: helm upgrade (stack + env overlay)"
helm upgrade --install "$HELM_RELEASE" "$CHART" -n "$HELM_NAMESPACE" --create-namespace \
  -f "$OVERLAY" \
  --set loadTest.runJob=false \
  --wait --timeout 10m

echo "==> waiting for api + mongodb"
kubectl wait --for=condition=ready pod -l app=api -n "$HELM_NAMESPACE" --timeout=300s
kubectl wait --for=condition=ready pod -l app=mongodb -n "$HELM_NAMESPACE" --timeout=300s || true

echo "==> TC${TC}: starting k6 Job"
helm upgrade "$HELM_RELEASE" "$CHART" -n "$HELM_NAMESPACE" \
  -f "$OVERLAY" \
  --set loadTest.runJob=true \
  --reuse-values

JOB="${HELM_RELEASE}-k6-e2e"
kubectl wait --for=condition=complete "job/$JOB" -n "$HELM_NAMESPACE" --timeout=30m || {
  kubectl logs -n "$HELM_NAMESPACE" "job/$JOB" --tail=200 || true
  exit 1
}

kubectl logs -n "$HELM_NAMESPACE" "job/$JOB"
kubectl delete job "$JOB" -n "$HELM_NAMESPACE" --ignore-not-found

helm upgrade "$HELM_RELEASE" "$CHART" -n "$HELM_NAMESPACE" \
  -f "$OVERLAY" \
  --set loadTest.runJob=false \
  --reuse-values

echo "==> TC${TC} done. pprof: kubectl port-forward -n $HELM_NAMESPACE deploy/${HELM_RELEASE}-api 6060:6060"
