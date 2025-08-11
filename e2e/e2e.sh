#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

# shellcheck disable=SC2317 # shellcheck does not understand that the cleanup() function is reached through a trap

set -eo pipefail

HELM_INSTALLATION_WITHOUT_OWNER_INFO="test-chart-without-owner-info"
HELM_INSTALLATION_WITH_OWNER_INFO_DEV="test-chart-with-owner-info"

HELM_CHART_NAME="test-chart"

NAMESPACE="owner-label-injector-staging"

function cleanup {
    echo "[INFO] Cleanup with removing the Helm installations.."
    helm --namespace "$NAMESPACE" delete --wait "$HELM_INSTALLATION_WITH_OWNER_INFO_DEV"
    helm --namespace "$NAMESPACE" delete --wait "$HELM_INSTALLATION_WITHOUT_OWNER_INFO"
}
trap cleanup EXIT

# namespace
# resource
# app.kubernetes.io/instance
# TODO: support group
# have labels
function check {
    labelCheckConditionInText="true"
    if $5 ; then
        labelCheckConditionInText="false"
    fi

    KUBECTL_ALL_CHECK=$(kubectl --namespace "$1"  -l "app.kubernetes.io/instance=$3" get "$2" -o json | jq '.items[] | .metadata.labels."ccloud/support-group" == "dev"')
    if [[ $KUBECTL_ALL_CHECK == *"$labelCheckConditionInText"* ]]; then
        echo "[ERROR] Some resources does not have correct labels"
        kubectl --namespace "$1" -l "app.kubernetes.io/instance=$3" -L ccloud/support-group -L ccloud/service get "$2"
        exit 1
    else
        echo "[INFO] kubectl get $2: resources have correct labels"
        kubectl --namespace "$1" -l "app.kubernetes.io/instance=$3" -L ccloud/support-group -L ccloud/service get "$2"
    fi
}



echo "----------"
echo "[INFO] Install Helm charts for testing.."
helm dep update $HELM_CHART_NAME
helm install  $HELM_INSTALLATION_WITHOUT_OWNER_INFO       --namespace $NAMESPACE  $HELM_CHART_NAME --set owner-info.enabled=false
helm install  $HELM_INSTALLATION_WITH_OWNER_INFO_DEV      --namespace $NAMESPACE  $HELM_CHART_NAME
echo "[INFO] Wait for cronjobs to be active.."
sleep 120
echo "----------"

echo "----------"
echo "[INFO] Check labels are assigned to resources"
echo "----------"

echo "[INFO] Helm chart $HELM_INSTALLATION_WITH_OWNER_INFO_DEV"
echo "----------"
check $NAMESPACE "all" $HELM_INSTALLATION_WITH_OWNER_INFO_DEV "dev" true
check $NAMESPACE "ingress" $HELM_INSTALLATION_WITH_OWNER_INFO_DEV "dev" true
echo "----------"

echo "[INFO] Helm chart $HELM_INSTALLATION_WITHOUT_OWNER_INFO"
echo "----------"
check $NAMESPACE "all" $HELM_INSTALLATION_WITHOUT_OWNER_INFO "dev" false
check $NAMESPACE "ingress" $HELM_INSTALLATION_WITHOUT_OWNER_INFO "dev" false
echo "----------"


echo "----------"
echo "[INFO] Upgrade deployments new image.."
kubectl set image deployment/$HELM_INSTALLATION_WITHOUT_OWNER_INFO   '*=nginx:1.19.4'
kubectl rollout status deployment/$HELM_INSTALLATION_WITHOUT_OWNER_INFO

kubectl set image deployment/$HELM_INSTALLATION_WITH_OWNER_INFO_DEV      '*=nginx:1.19.4'
kubectl rollout status deployment/$HELM_INSTALLATION_WITH_OWNER_INFO_DEV 

echo "----------"

echo "----------"
echo "[INFO] Check labels are assigned to resources"
echo "----------"

echo "[INFO] Helm chart $HELM_INSTALLATION_WITH_OWNER_INFO_DEV"
echo "----------"
check $NAMESPACE "all" $HELM_INSTALLATION_WITH_OWNER_INFO_DEV "dev" true
check $NAMESPACE "ingress" $HELM_INSTALLATION_WITH_OWNER_INFO_DEV "dev" true
echo "----------"

echo "[INFO] Helm chart $HELM_INSTALLATION_WITHOUT_OWNER_INFO"
echo "----------"
check $NAMESPACE "all" $HELM_INSTALLATION_WITHOUT_OWNER_INFO "dev" false
check $NAMESPACE "ingress" $HELM_INSTALLATION_WITHOUT_OWNER_INFO "dev" false
echo "----------"


echo "[SUCCESS] Tests are done!"

exit 0
