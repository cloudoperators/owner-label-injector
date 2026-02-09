#!/usr/bin/env bash
set -eo pipefail

CLUSTER_NAME=${KIND_CLUSTER_NAME:-oli-e2e}

echo "[INFO] Deleting KinD cluster: $CLUSTER_NAME"
kind delete cluster --name "$CLUSTER_NAME"
echo "[SUCCESS] Cluster deleted"
