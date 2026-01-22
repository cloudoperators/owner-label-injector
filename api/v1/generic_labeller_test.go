// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package v1_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiV1 "github.com/cloudoperators/owner-label-injector/api/v1"

	appsv1 "k8s.io/api/apps/v1"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("The webhook", Ordered, func() {
	resourceName := types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "test-secret"}

	It("appends owner-info from a configmap when rules are configured", func(ctx SpecContext) {
		var staticSecret corev1.Secret
		staticSecret.Name = "static-secret"
		staticSecret.Namespace = resourceName.Namespace
		staticSecret.Type = corev1.SecretTypeOpaque
		staticSecret.Labels = map[string]string{
			apiV1.HelmLabelKey: apiV1.HelmLabelValue,
		}
		staticSecret.Annotations = map[string]string{
			apiV1.HelmReleaseNameAnnotation:      "static-release",
			apiV1.HelmReleaseNamespaceAnnotation: metav1.NamespaceDefault,
		}
		Expect(k8sClient.Create(ctx, &staticSecret)).To(Succeed())

		var result corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&staticSecret), &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.SupportGroupKey(), "static-group"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.ServiceKey(), "static-service"))
	})

	It("appends owner-info from a configmap when a resource is owned by helm", func(ctx SpecContext) {
		var ownerConfigmap corev1.ConfigMap
		ownerConfigmap.Name = "owner-of-test-chart"
		ownerConfigmap.Namespace = metav1.NamespaceDefault
		ownerConfigmap.Data = map[string]string{
			testConfig.Helm.SupportGroupDataKey: "experts",
			testConfig.Helm.ServiceDataKey:      "cool-service",
		}
		Expect(k8sClient.Create(ctx, &ownerConfigmap)).To(Succeed())

		var resource corev1.Secret
		resource.Name = resourceName.Name
		resource.Namespace = resourceName.Namespace
		resource.Type = corev1.SecretTypeOpaque
		resource.Labels = map[string]string{
			apiV1.HelmLabelKey: apiV1.HelmLabelValue,
		}
		resource.Annotations = map[string]string{
			apiV1.HelmReleaseNameAnnotation:      "test-chart",
			apiV1.HelmReleaseNamespaceAnnotation: metav1.NamespaceDefault,
		}
		Expect(k8sClient.Create(ctx, &resource)).To(Succeed())

		var result corev1.Secret
		Expect(k8sClient.Get(ctx, resourceName, &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.SupportGroupKey(), "experts"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.ServiceKey(), "cool-service"))
	})

	It("appends owner-info from owner references", func(ctx SpecContext) {
		var owner corev1.Secret
		Expect(k8sClient.Get(ctx, resourceName, &owner)).To(Succeed())

		var owned corev1.Secret
		owned.Name = "owned-secret"
		owned.Namespace = metav1.NamespaceDefault
		owned.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: "v1",
				Kind:       "Secret",
				Name:       owner.Name,
				UID:        owner.UID,
			},
		}
		Expect(k8sClient.Create(ctx, &owned)).To(Succeed())

		var result corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&owned), &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.SupportGroupKey(), "experts"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.ServiceKey(), "cool-service"))
	})

	It("adds owner from ingress when resource is claimed by ingress via vice-president", func(ctx SpecContext) {
		var ingress networkingv1.Ingress
		ingress.Name = "the-ingress"
		ingress.Labels = map[string]string{
			testConfig.Labels.SupportGroupKey(): "supporters",
			testConfig.Labels.ServiceKey():      "ingress",
		}
		ingress.Namespace = metav1.NamespaceDefault
		ingress.Spec.DefaultBackend = &networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: "backend",
				Port: networkingv1.ServiceBackendPort{
					Number: 80,
				},
			},
		}
		Expect(k8sClient.Create(ctx, &ingress)).To(Succeed())

		var claimed corev1.Secret
		claimed.Name = "claimed"
		claimed.Namespace = metav1.NamespaceDefault
		claimed.Type = corev1.SecretTypeOpaque
		claimed.Annotations = map[string]string{
			testConfig.Traversal.VicePresidentAnnotationKey: metav1.NamespaceDefault + "/the-ingress",
		}
		Expect(k8sClient.Create(ctx, &claimed)).To(Succeed())

		var result corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&claimed), &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.SupportGroupKey(), "supporters"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.ServiceKey(), "ingress"))
	})

	It("pulls owner info for early-owner-of configmaps from actual owner-data configmap if it exists", func(ctx SpecContext) {
		var early corev1.ConfigMap
		early.Name = "early-owner-of-test-chart"
		early.Namespace = metav1.NamespaceDefault
		early.Data = map[string]string{
			testConfig.Helm.SupportGroupDataKey: "early-experts",
			testConfig.Helm.ServiceDataKey:      "early-service",
		}
		Expect(k8sClient.Create(ctx, &early)).To(Succeed())

		var result corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&early), &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.SupportGroupKey(), "experts"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.ServiceKey(), "cool-service"))
	})

	It("pulls owner-info for PVCs from StatefulSets", func(ctx SpecContext) {
		var statefulSet appsv1.StatefulSet
		statefulSet.Name = "persistent"
		statefulSet.Namespace = metav1.NamespaceDefault
		statefulSet.Labels = map[string]string{
			testConfig.Labels.SupportGroupKey(): "storage",
			testConfig.Labels.ServiceKey():      "sql",
		}
		statefulSet.Spec.Replicas = ptr.To(int32(2))
		statefulSet.Spec.Template.Labels = map[string]string{"selector": "val"}
		statefulSet.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"selector": "val"}}
		statefulSet.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:  "container",
				Image: "nginx",
			},
		}
		pvcSpec := corev1.PersistentVolumeClaimSpec{
			VolumeName:  "disk",
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					"storage": resource.MustParse("1Gi"),
				},
			},
		}
		statefulSet.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
			{
				Spec:       pvcSpec,
				ObjectMeta: metav1.ObjectMeta{Name: "disk"},
			},
		}
		Expect(k8sClient.Create(ctx, &statefulSet)).To(Succeed())

		var claim corev1.PersistentVolumeClaim
		claim.Name = "disk-persistent-0"
		claim.Namespace = metav1.NamespaceDefault
		claim.Spec = pvcSpec
		Expect(k8sClient.Create(ctx, &claim)).To(Succeed())

		var resultPVC corev1.PersistentVolumeClaim
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&claim), &resultPVC)).To(Succeed())
		var resultSts appsv1.StatefulSet
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&statefulSet), &resultSts)).To(Succeed())

		Expect(resultPVC.Labels).To(HaveKeyWithValue(testConfig.Labels.SupportGroupKey(), "storage"))
		Expect(resultPVC.Labels).To(HaveKeyWithValue(testConfig.Labels.ServiceKey(), "sql"))

		Expect(resultSts.Spec.Template.Labels).To(HaveKeyWithValue(testConfig.Labels.SupportGroupKey(), "storage"))
		Expect(resultSts.Spec.Template.Labels).To(HaveKeyWithValue(testConfig.Labels.ServiceKey(), "sql"))
	})

	It("appends owner-info from Helm release secret when global.greenhouse.ownedBy is set", func(ctx SpecContext) {
		helmRelease := map[string]interface{}{
			"config": map[string]interface{}{
				"global": map[string]interface{}{
					"greenhouse": map[string]interface{}{
						"ownedBy": "greenhouse-team",
					},
				},
			},
		}

		releaseJSON, err := json.Marshal(helmRelease)
		Expect(err).NotTo(HaveOccurred())

		var buf bytes.Buffer
		gzipWriter := gzip.NewWriter(&buf)
		_, err = gzipWriter.Write(releaseJSON)
		Expect(err).NotTo(HaveOccurred())
		Expect(gzipWriter.Close()).To(Succeed())

		encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

		var helmSecret corev1.Secret
		helmSecret.Name = "sh.helm.release.v1.greenhouse-release.v1"
		helmSecret.Namespace = metav1.NamespaceDefault
		helmSecret.Type = "helm.sh/release.v1"
		helmSecret.Data = map[string][]byte{
			"release": []byte(encoded),
		}
		Expect(k8sClient.Create(ctx, &helmSecret)).To(Succeed())

		var resource corev1.Secret
		resource.Name = "greenhouse-managed-secret"
		resource.Namespace = metav1.NamespaceDefault
		resource.Type = corev1.SecretTypeOpaque
		resource.Labels = map[string]string{
			apiV1.HelmLabelKey: apiV1.HelmLabelValue,
		}
		resource.Annotations = map[string]string{
			apiV1.HelmReleaseNameAnnotation:      "greenhouse-release",
			apiV1.HelmReleaseNamespaceAnnotation: metav1.NamespaceDefault,
		}
		Expect(k8sClient.Create(ctx, &resource)).To(Succeed())

		var result corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&resource), &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.SupportGroupKey(), "greenhouse-team"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.ServiceKey(), "greenhouse-release"))
	})

	It("prefers Helm release secret over ConfigMap when both exist", func(ctx SpecContext) {
		var ownerConfigmap corev1.ConfigMap
		ownerConfigmap.Name = "owner-of-priority-release"
		ownerConfigmap.Namespace = metav1.NamespaceDefault
		ownerConfigmap.Data = map[string]string{
			testConfig.Helm.SupportGroupDataKey: "configmap-team",
			testConfig.Helm.ServiceDataKey:      "configmap-service",
		}
		Expect(k8sClient.Create(ctx, &ownerConfigmap)).To(Succeed())

		helmRelease := map[string]interface{}{
			"config": map[string]interface{}{
				"global": map[string]interface{}{
					"greenhouse": map[string]interface{}{
						"ownedBy": "helm-secret-team",
					},
				},
			},
		}

		releaseJSON, err := json.Marshal(helmRelease)
		Expect(err).NotTo(HaveOccurred())

		var buf bytes.Buffer
		gzipWriter := gzip.NewWriter(&buf)
		_, err = gzipWriter.Write(releaseJSON)
		Expect(err).NotTo(HaveOccurred())
		Expect(gzipWriter.Close()).To(Succeed())

		encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

		var helmSecret corev1.Secret
		helmSecret.Name = "sh.helm.release.v1.priority-release.v1"
		helmSecret.Namespace = metav1.NamespaceDefault
		helmSecret.Type = "helm.sh/release.v1"
		helmSecret.Data = map[string][]byte{
			"release": []byte(encoded),
		}
		Expect(k8sClient.Create(ctx, &helmSecret)).To(Succeed())

		var resource corev1.Secret
		resource.Name = "priority-test-secret"
		resource.Namespace = metav1.NamespaceDefault
		resource.Type = corev1.SecretTypeOpaque
		resource.Labels = map[string]string{
			apiV1.HelmLabelKey: apiV1.HelmLabelValue,
		}
		resource.Annotations = map[string]string{
			apiV1.HelmReleaseNameAnnotation:      "priority-release",
			apiV1.HelmReleaseNamespaceAnnotation: metav1.NamespaceDefault,
		}
		Expect(k8sClient.Create(ctx, &resource)).To(Succeed())

		var result corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&resource), &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.SupportGroupKey(), "helm-secret-team"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.Labels.ServiceKey(), "priority-release"))
	})
})
