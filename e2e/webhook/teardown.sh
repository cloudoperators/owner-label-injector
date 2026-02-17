#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
# SPDX-License-Identifier: Apache-2.0

set -eo pipefail

CLUSTER_NAME=${KIND_CLUSTER_NAME:-oli-e2e}

echo "[INFO] Deleting KinD cluster: $CLUSTER_NAME"
kind delete cluster --name "$CLUSTER_NAME"
echo "[SUCCESS] Cluster deleted"
