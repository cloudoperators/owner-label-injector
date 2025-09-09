// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"strings"

	"github.com/cloudoperators/owner-label-injector/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const HelmLabelKey = "app.kubernetes.io/managed-by"
const HelmLabelValue = "helm"
const HelmReleaseNameAnnotation = "meta.helm.sh/release-name"
const HelmReleaseNamespaceAnnotation = "meta.helm.sh/release-namespace"

func IsManagedByHelm(labels, annotations map[string]string) (isManagedByHelm bool, release, releaseNamespace string) {
	// check managed by label
	manager, ok := labels[HelmLabelKey]
	if !ok || strings.ToLower(manager) != HelmLabelValue {
		return false, "", ""
	}

	// get helm release name from annotation
	release, releaseOK := annotations[HelmReleaseNameAnnotation]
	releaseNamespace, releaseNamespaceOK := annotations[HelmReleaseNamespaceAnnotation]
	if !releaseOK || !releaseNamespaceOK {
		return false, "", ""
	}

	return true, release, releaseNamespace
}

func GetOwnerDataFromLabels(labels map[string]string, cfg *config.Global) (bool, OwnerData) {
	supportGroup, ok := labels[cfg.Labels.SupportGroupKey()]
	if !ok {
		return false, OwnerData{}
	}

	return true, OwnerData{SupportGroup: supportGroup, Service: labels[cfg.Labels.ServiceKey()]}
}

const OwnerConfigmapDatasource = "owner-info"

func GetOwnerDataFromOwnerConfigmap(c client.Client, cfg *config.Global, releaseName, releaseNamespace string) (OwnerData, bool, error) {
	cm := &corev1.ConfigMap{}
	err := c.Get(context.TODO(), types.NamespacedName{Namespace: releaseNamespace, Name: cfg.Helm.OwnerConfigMapPrefix + releaseName}, cm)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// try the configmap deployed with helm hooks
			err = c.Get(context.TODO(), types.NamespacedName{Namespace: releaseNamespace, Name: cfg.Helm.OwnerConfigMapFallbackPrefix + releaseName}, cm)
			if k8serrors.IsNotFound(err) {
				return OwnerData{}, false, nil
			}
			return OwnerData{}, false, err
		}
		return OwnerData{}, false, err
	}

	supportGroup, supportGroupOK := cm.Data[cfg.Helm.SupportGroupDataKey]
	service := cm.Data[cfg.Helm.ServiceDataKey]

	if !supportGroupOK {
		return OwnerData{}, false, errors.New("missing data in owner configmap")
	}

	return OwnerData{Service: service, SupportGroup: supportGroup, DataSource: OwnerConfigmapDatasource}, true, nil
}

func WorkloadAPILabeller(object *unstructured.Unstructured, ownerData OwnerData, cfg *config.Global) (*unstructured.Unstructured, bool, error) {
	// If there is an owner, skip it
	ownerReferences := object.GetOwnerReferences()
	if len(ownerReferences) > 0 {
		return object, false, nil
	}

	// If there is a pod template hash, skip it
	labels := object.GetLabels()
	if labels != nil {
		_, ok := labels["pod-template-hash"]
		if ok {
			return object, false, nil
		}
	}

	// Try to find the location of pod spec metadata
	locationBuilder := []string{"spec"}
	if strings.ToLower(object.GetKind()) == "cronjob" {
		locationBuilder = append(locationBuilder, "jobTemplate", "spec", "template", "metadata")
	} else {
		locationBuilder = append(locationBuilder, "template", "metadata")
	}

	_, podSpecTemplateMetadataFound, err := unstructured.NestedMap(object.Object, locationBuilder...)
	if err != nil {
		return nil, false, err
	}

	if podSpecTemplateMetadataFound {
		// now find the labels
		locationBuilder = append(locationBuilder, "labels")
		podSpecTemplateLabels, podTemplateLabelsFound, err := unstructured.NestedStringMap(object.Object, locationBuilder...)
		if err != nil {
			return nil, false, err
		}

		// if labels are found add owner data labels
		if podTemplateLabelsFound {
			maps.Copy(podSpecTemplateLabels, ownerData.Labels(cfg))
			err = unstructured.SetNestedStringMap(object.Object, podSpecTemplateLabels, locationBuilder...)
		} else { // else inject labels if there is not any before
			err = unstructured.SetNestedStringMap(object.Object, ownerData.Labels(cfg), locationBuilder...)
		}
		return object, true, err
	}

	return object, false, nil
}

func UpwardTraverseGetOwnerData(ctx context.Context, k8sClient client.Client, cfg *config.Global, namespace string, object *unstructured.Unstructured, forceCheck bool) (OwnerData, bool, error) {
	annotations := object.GetAnnotations()
	labels := object.GetLabels()
	ownerReferences := object.GetOwnerReferences()

	// (1) check if it has owner labels itself
	found, ownerData := GetOwnerDataFromLabels(labels, cfg)
	if found && !forceCheck {
		return ownerData, true, nil
	}

	// check if it is managed by helm
	managedByHelm, release, releaseNamespace := IsManagedByHelm(labels, annotations)

	// if it is managed by helm, check for its owner-info chart
	if managedByHelm {
		ownerDataFromOwnerConfigmap, found, err := GetOwnerDataFromOwnerConfigmap(k8sClient, cfg, release, releaseNamespace)
		if found {
			return ownerDataFromOwnerConfigmap, true, nil
		}
		if err != nil {
			return ownerDataFromOwnerConfigmap, false, err
		}
		// Check static rules from environment variables.
		if len(cfg.StaticRules.Rules) > 0 {
			found, staticMatch := cfg.StaticRules.Check(release, releaseNamespace)
			if found {
				return OwnerData{
					SupportGroup: staticMatch.SupportGroup,
					Service:      staticMatch.Service,
					DataSource:   staticMatch.DataSource,
				}, true, nil
			}
		}
	}

	ownerAPIVersion := ""
	ownerKind := ""
	ownerName := ""
	ownerNamespace := ""

	// if it not managey by helm, check if it has owner references
	// if it has owner references, try (1) with the owner
	if len(ownerReferences) == 0 {
		// SPECIAL CASES FOR OWNER DISCOVERY

		// SPECIAL CASE 1: TLS certs
		// annotation: vice-president/claimed-by-ingress
		if annotations != nil && annotations[cfg.Traversal.VicePresidentAnnotationKey] != "" {
			ingress := annotations[cfg.Traversal.VicePresidentAnnotationKey]
			ingressData := strings.Split(ingress, "/")

			if len(ingressData) == 2 {
				ownerAPIVersion = "networking.k8s.io/v1"
				ownerKind = "Ingress"
				ownerName = ingressData[1]
				ownerNamespace = ingressData[0]
			}
		}

		// SPECIAL CASE 2: early-owner-info-owner-of-X configmap
		if strings.HasPrefix(object.GetName(), cfg.Helm.OwnerConfigMapFallbackPrefix) && object.GetKind() == "ConfigMap" {
			release := strings.TrimPrefix(object.GetName(), cfg.Helm.OwnerConfigMapFallbackPrefix)
			ownerDataFromOwnerConfigmap, found, err := GetOwnerDataFromOwnerConfigmap(k8sClient, cfg, release, namespace)
			if found {
				return ownerDataFromOwnerConfigmap, true, nil
			}
			if err != nil {
				return ownerDataFromOwnerConfigmap, false, err
			}
		}

		// SPECIAL CASE 3: VerticalPodAutoscalerCheckpoints
		if object.GetKind() == "VerticalPodAutoscalerCheckpoint" {
			vpa, found, err := unstructured.NestedString(object.Object, "spec", "vpaObjectName")
			if err != nil {
				return OwnerData{}, false, err
			}
			if found {
				ownerAPIVersion = "autoscaling.k8s.io/v1"
				ownerKind = "VerticalPodAutoscaler"
				ownerName = vpa
				ownerNamespace = namespace
			}
		}

		// SPECIAL CASE 4: PersistentVolumeClaims
		if object.GetKind() == "PersistentVolumeClaim" {
			var statefulSets appsv1.StatefulSetList
			err := k8sClient.List(context.TODO(), &statefulSets, client.InNamespace(object.GetNamespace()))
			if err != nil {
				return OwnerData{}, false, err
			}
			for _, statefulSet := range statefulSets.Items {
				for _, volumeClaimTemplate := range statefulSet.Spec.VolumeClaimTemplates {
					pvcIndex, ok := strings.CutPrefix(object.GetName(), volumeClaimTemplate.Name+"-"+statefulSet.Name+"-")
					_, parseErr := strconv.Atoi(pvcIndex)
					if ok && parseErr == nil {
						ownerAPIVersion = "apps/v1"
						ownerKind = "StatefulSet"
						ownerName = statefulSet.Name
						ownerNamespace = statefulSet.Namespace
					}
				}
			}
		}

		// No special case found
		if ownerName == "" { // just check one field as they are filled together
			return OwnerData{}, false, nil
		}
	} else {
		ownerAPIVersion = ownerReferences[0].APIVersion
		ownerKind = ownerReferences[0].Kind
		ownerName = ownerReferences[0].Name
		ownerNamespace = namespace

		// SPECIAL CASE 4: Outdated owner references are not updated
		// https://github.com/kubernetes/kubernetes/issues/96650
		if ownerKind == "Ingress" && ownerAPIVersion == "extensions/v1beta1" {
			ownerAPIVersion = "networking.k8s.io/v1"
		}
		if ownerKind == "Deployment" && ownerAPIVersion == "extensions/v1beta1" {
			ownerAPIVersion = "apps/v1"
		}
		if ownerKind == "DaemonSet" && ownerAPIVersion == "extensions/v1beta1" {
			ownerAPIVersion = "apps/v1"
		}
	}

	// get owner reference
	gvk := schema.FromAPIVersionAndKind(ownerAPIVersion, ownerKind)
	owner := &unstructured.Unstructured{}
	owner.SetGroupVersionKind(gvk)
	owner.SetName(ownerName)
	owner.SetNamespace(ownerNamespace)

	err := k8sClient.Get(ctx, types.NamespacedName{Name: owner.GetName(), Namespace: ownerNamespace}, owner)
	if err != nil {
		return ownerData, false, err
	}

	return UpwardTraverseGetOwnerData(ctx, k8sClient, cfg, ownerNamespace, owner, forceCheck)
}
