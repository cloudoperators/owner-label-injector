#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
# SPDX-License-Identifier: Apache-2.0

set -eo pipefail

CLUSTER_NAME=${KIND_CLUSTER_NAME:-oli-e2e}
NAMESPACE=${E2E_NAMESPACE:-oli-e2e}
IMG=${IMG:-owner-label-injector:e2e}
CERT_MANAGER_VERSION=${CERT_MANAGER_VERSION:-v1.17.2}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OLI_CHART="$PROJECT_ROOT/charts/owner-label-injector"

echo "[INFO] Creating KinD cluster: $CLUSTER_NAME"
kind create cluster --name "$CLUSTER_NAME" --wait 5m

echo "[INFO] Installing cert-manager $CERT_MANAGER_VERSION"
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"

echo "[INFO] Waiting for cert-manager to be ready..."
kubectl wait --for=condition=Available -n cert-manager deployment/cert-manager --timeout=120s
kubectl wait --for=condition=Available -n cert-manager deployment/cert-manager-cainjector --timeout=120s
kubectl wait --for=condition=Available -n cert-manager deployment/cert-manager-webhook --timeout=120s

echo "[INFO] Building Docker image: $IMG"
docker build -t "$IMG" "$PROJECT_ROOT"

echo "[INFO] Loading image into KinD cluster"
kind load docker-image "$IMG" --name "$CLUSTER_NAME"

echo "[INFO] Deploying OLI via Helm chart"
helm install owner-label-injector "$OLI_CHART" \
  --namespace owner-label-injector-system \
  --create-namespace \
  --set image.repository=owner-label-injector \
  --set image.tag=e2e \
  --set image.pullPolicy=Never \
  --set replicaCount=1 \
  --set cronjob.enabled=false \
  --set resources.requests.cpu=10m \
  --set resources.requests.memory=64Mi \
  --set resources.limits.cpu=500m \
  --set resources.limits.memory=128Mi \
  --wait --timeout 120s

echo "[INFO] Creating test namespace: $NAMESPACE"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

echo "[SUCCESS] E2E environment is ready"
echo "  Cluster:   $CLUSTER_NAME"
echo "  Namespace: $NAMESPACE"
echo "  Image:     $IMG"
