// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createEncodedHelmRelease builds a Helm release payload (JSON -> gzip -> base64).
func createEncodedHelmRelease(ownedBy string) []byte {
	config := map[string]any{}
	if ownedBy != "" {
		config["global"] = map[string]any{
			"greenhouse": map[string]any{
				"ownedBy": ownedBy,
			},
		}
	}
	helmRelease := map[string]any{"config": config}

	releaseJSON, err := json.Marshal(helmRelease)
	Expect(err).NotTo(HaveOccurred())

	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	_, err = gzipWriter.Write(releaseJSON)
	Expect(err).NotTo(HaveOccurred())
	Expect(gzipWriter.Close()).To(Succeed())

	return []byte(base64.StdEncoding.EncodeToString(buf.Bytes()))
}

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
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "static-group"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "static-service"))
	})

	It("appends owner-info from a configmap when a resource is owned by helm", func(ctx SpecContext) {
		var ownerConfigmap corev1.ConfigMap
		ownerConfigmap.Name = "owner-of-test-chart"
		ownerConfigmap.Namespace = metav1.NamespaceDefault
		ownerConfigmap.Data = map[string]string{
			testConfig.SupportGroupDataKey: "experts",
			testConfig.ServiceDataKey:      "cool-service",
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
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "experts"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "cool-service"))
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
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "experts"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "cool-service"))
	})

	It("adds owner from ingress when resource is claimed by ingress via vice-president", func(ctx SpecContext) {
		var ingress networkingv1.Ingress
		ingress.Name = "the-ingress"
		ingress.Labels = map[string]string{
			testConfig.SupportGroupKey(): "supporters",
			testConfig.ServiceKey():      "ingress",
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
			testConfig.VicePresidentAnnotationKey: metav1.NamespaceDefault + "/the-ingress",
		}
		Expect(k8sClient.Create(ctx, &claimed)).To(Succeed())

		var result corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&claimed), &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "supporters"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "ingress"))
	})

	It("pulls owner info for early-owner-of configmaps from actual owner-data configmap if it exists", func(ctx SpecContext) {
		var early corev1.ConfigMap
		early.Name = "early-owner-of-test-chart"
		early.Namespace = metav1.NamespaceDefault
		early.Data = map[string]string{
			testConfig.SupportGroupDataKey: "early-experts",
			testConfig.ServiceDataKey:      "early-service",
		}
		Expect(k8sClient.Create(ctx, &early)).To(Succeed())

		var result corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&early), &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "experts"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "cool-service"))
	})

	It("pulls owner-info for PVCs from StatefulSets", func(ctx SpecContext) {
		var statefulSet appsv1.StatefulSet
		statefulSet.Name = "persistent"
		statefulSet.Namespace = metav1.NamespaceDefault
		statefulSet.Labels = map[string]string{
			testConfig.SupportGroupKey(): "storage",
			testConfig.ServiceKey():      "sql",
		}
		statefulSet.Spec.Replicas = new(int32(2))
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

		Expect(resultPVC.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "storage"))
		Expect(resultPVC.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "sql"))

		Expect(resultSts.Spec.Template.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "storage"))
		Expect(resultSts.Spec.Template.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "sql"))
	})

	It("appends owner-info from Helm release secret when global.greenhouse.ownedBy is set", func(ctx SpecContext) {
		var helmSecret corev1.Secret
		helmSecret.Name = "sh.helm.release.v1.greenhouse-release.v1"
		helmSecret.Namespace = metav1.NamespaceDefault
		helmSecret.Type = "helm.sh/release.v1"
		helmSecret.Data = map[string][]byte{
			"release": createEncodedHelmRelease("greenhouse-team"),
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
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "greenhouse-team"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "greenhouse-release"))
		Expect(result.Annotations).To(HaveKeyWithValue(testConfig.DataSourceAnnotation(), "helm-release-secret"))
	})

	It("prefers Helm release secret over ConfigMap when both exist", func(ctx SpecContext) {
		var ownerConfigmap corev1.ConfigMap
		ownerConfigmap.Name = "owner-of-priority-release"
		ownerConfigmap.Namespace = metav1.NamespaceDefault
		ownerConfigmap.Data = map[string]string{
			testConfig.SupportGroupDataKey: "configmap-team",
			testConfig.ServiceDataKey:      "configmap-service",
		}
		Expect(k8sClient.Create(ctx, &ownerConfigmap)).To(Succeed())

		var helmSecret corev1.Secret
		helmSecret.Name = "sh.helm.release.v1.priority-release.v1"
		helmSecret.Namespace = metav1.NamespaceDefault
		helmSecret.Type = "helm.sh/release.v1"
		helmSecret.Data = map[string][]byte{
			"release": createEncodedHelmRelease("helm-secret-team"),
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
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "helm-secret-team"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "priority-release"))
	})

	It("reads latest Helm release secret version when multiple exist at create time", func(ctx SpecContext) {
		var helmSecretV1 corev1.Secret
		helmSecretV1.Name = "sh.helm.release.v1.multi-ver.v1"
		helmSecretV1.Namespace = metav1.NamespaceDefault
		helmSecretV1.Type = "helm.sh/release.v1"
		helmSecretV1.Data = map[string][]byte{
			"release": createEncodedHelmRelease("team-v1"),
		}
		Expect(k8sClient.Create(ctx, &helmSecretV1)).To(Succeed())

		var helmSecretV2 corev1.Secret
		helmSecretV2.Name = "sh.helm.release.v1.multi-ver.v2"
		helmSecretV2.Namespace = metav1.NamespaceDefault
		helmSecretV2.Type = "helm.sh/release.v1"
		helmSecretV2.Data = map[string][]byte{
			"release": createEncodedHelmRelease("team-v2"),
		}
		Expect(k8sClient.Create(ctx, &helmSecretV2)).To(Succeed())

		var resource corev1.Secret
		resource.Name = "multi-ver-resource"
		resource.Namespace = metav1.NamespaceDefault
		resource.Type = corev1.SecretTypeOpaque
		resource.Labels = map[string]string{
			apiV1.HelmLabelKey: apiV1.HelmLabelValue,
		}
		resource.Annotations = map[string]string{
			apiV1.HelmReleaseNameAnnotation:      "multi-ver",
			apiV1.HelmReleaseNamespaceAnnotation: metav1.NamespaceDefault,
		}
		Expect(k8sClient.Create(ctx, &resource)).To(Succeed())

		var result corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&resource), &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "team-v2"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "multi-ver"))
		Expect(result.Annotations).To(HaveKeyWithValue(testConfig.DataSourceAnnotation(), "helm-release-secret"))
	})

	It("falls back to ConfigMap when Helm release secret lacks greenhouse.ownedBy", func(ctx SpecContext) {
		var ownerConfigmap corev1.ConfigMap
		ownerConfigmap.Name = "owner-of-no-gh-release"
		ownerConfigmap.Namespace = metav1.NamespaceDefault
		ownerConfigmap.Data = map[string]string{
			testConfig.SupportGroupDataKey: "cm-team",
			testConfig.ServiceDataKey:      "cm-service",
		}
		Expect(k8sClient.Create(ctx, &ownerConfigmap)).To(Succeed())

		var helmSecret corev1.Secret
		helmSecret.Name = "sh.helm.release.v1.no-gh-release.v1"
		helmSecret.Namespace = metav1.NamespaceDefault
		helmSecret.Type = "helm.sh/release.v1"
		helmSecret.Data = map[string][]byte{
			"release": createEncodedHelmRelease(""),
		}
		Expect(k8sClient.Create(ctx, &helmSecret)).To(Succeed())

		var resource corev1.Secret
		resource.Name = "no-gh-resource"
		resource.Namespace = metav1.NamespaceDefault
		resource.Type = corev1.SecretTypeOpaque
		resource.Labels = map[string]string{
			apiV1.HelmLabelKey: apiV1.HelmLabelValue,
		}
		resource.Annotations = map[string]string{
			apiV1.HelmReleaseNameAnnotation:      "no-gh-release",
			apiV1.HelmReleaseNamespaceAnnotation: metav1.NamespaceDefault,
		}
		Expect(k8sClient.Create(ctx, &resource)).To(Succeed())

		var result corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&resource), &result)).To(Succeed())
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "cm-team"))
		Expect(result.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "cm-service"))
		Expect(result.Annotations).To(HaveKeyWithValue(testConfig.DataSourceAnnotation(), "owner-info"))
	})

	It("retains original labels on update even when Helm release secret changes (sticky labels)", func(ctx SpecContext) {
		var helmSecretV1 corev1.Secret
		helmSecretV1.Name = "sh.helm.release.v1.sticky-release.v1"
		helmSecretV1.Namespace = metav1.NamespaceDefault
		helmSecretV1.Type = "helm.sh/release.v1"
		helmSecretV1.Data = map[string][]byte{
			"release": createEncodedHelmRelease("team-alpha"),
		}
		Expect(k8sClient.Create(ctx, &helmSecretV1)).To(Succeed())

		var resource corev1.Secret
		resource.Name = "sticky-resource"
		resource.Namespace = metav1.NamespaceDefault
		resource.Type = corev1.SecretTypeOpaque
		resource.Labels = map[string]string{
			apiV1.HelmLabelKey: apiV1.HelmLabelValue,
		}
		resource.Annotations = map[string]string{
			apiV1.HelmReleaseNameAnnotation:      "sticky-release",
			apiV1.HelmReleaseNamespaceAnnotation: metav1.NamespaceDefault,
		}
		Expect(k8sClient.Create(ctx, &resource)).To(Succeed())

		var created corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&resource), &created)).To(Succeed())
		Expect(created.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "team-alpha"))

		var helmSecretV2 corev1.Secret
		helmSecretV2.Name = "sh.helm.release.v1.sticky-release.v2"
		helmSecretV2.Namespace = metav1.NamespaceDefault
		helmSecretV2.Type = "helm.sh/release.v1"
		helmSecretV2.Data = map[string][]byte{
			"release": createEncodedHelmRelease("team-beta"),
		}
		Expect(k8sClient.Create(ctx, &helmSecretV2)).To(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&resource), &created)).To(Succeed())
		if created.Data == nil {
			created.Data = map[string][]byte{}
		}
		created.Data["updated"] = []byte("true")
		Expect(k8sClient.Update(ctx, &created)).To(Succeed())

		var updated corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&resource), &updated)).To(Succeed())
		Expect(updated.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "team-alpha"))
		Expect(updated.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "sticky-release"))
	})

	It("retains ConfigMap-sourced labels on update even when new Helm release secret is added", func(ctx SpecContext) {
		var ownerConfigmap corev1.ConfigMap
		ownerConfigmap.Name = "owner-of-fallback-rel"
		ownerConfigmap.Namespace = metav1.NamespaceDefault
		ownerConfigmap.Data = map[string]string{
			testConfig.SupportGroupDataKey: "cm-team",
			testConfig.ServiceDataKey:      "cm-service",
		}
		Expect(k8sClient.Create(ctx, &ownerConfigmap)).To(Succeed())

		var helmSecretV1 corev1.Secret
		helmSecretV1.Name = "sh.helm.release.v1.fallback-rel.v1"
		helmSecretV1.Namespace = metav1.NamespaceDefault
		helmSecretV1.Type = "helm.sh/release.v1"
		helmSecretV1.Data = map[string][]byte{
			"release": createEncodedHelmRelease(""),
		}
		Expect(k8sClient.Create(ctx, &helmSecretV1)).To(Succeed())

		var resource corev1.Secret
		resource.Name = "fallback-sticky-resource"
		resource.Namespace = metav1.NamespaceDefault
		resource.Type = corev1.SecretTypeOpaque
		resource.Labels = map[string]string{
			apiV1.HelmLabelKey: apiV1.HelmLabelValue,
		}
		resource.Annotations = map[string]string{
			apiV1.HelmReleaseNameAnnotation:      "fallback-rel",
			apiV1.HelmReleaseNamespaceAnnotation: metav1.NamespaceDefault,
		}
		Expect(k8sClient.Create(ctx, &resource)).To(Succeed())

		var created corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&resource), &created)).To(Succeed())
		Expect(created.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "cm-team"))
		Expect(created.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "cm-service"))
		Expect(created.Annotations).To(HaveKeyWithValue(testConfig.DataSourceAnnotation(), "owner-info"))

		var helmSecretV2 corev1.Secret
		helmSecretV2.Name = "sh.helm.release.v1.fallback-rel.v2"
		helmSecretV2.Namespace = metav1.NamespaceDefault
		helmSecretV2.Type = "helm.sh/release.v1"
		helmSecretV2.Data = map[string][]byte{
			"release": createEncodedHelmRelease("greenhouse-team"),
		}
		Expect(k8sClient.Create(ctx, &helmSecretV2)).To(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&resource), &created)).To(Succeed())
		if created.Data == nil {
			created.Data = map[string][]byte{}
		}
		created.Data["updated"] = []byte("true")
		Expect(k8sClient.Update(ctx, &created)).To(Succeed())

		var updated corev1.Secret
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&resource), &updated)).To(Succeed())
		Expect(updated.Labels).To(HaveKeyWithValue(testConfig.SupportGroupKey(), "cm-team"))
		Expect(updated.Labels).To(HaveKeyWithValue(testConfig.ServiceKey(), "cm-service"))
	})
})
