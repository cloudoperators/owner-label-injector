// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

//go:build webhookE2E

package webhook

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Owner Label Injection with Helm Releases", Ordered, func() {

	Context("helm install with global.greenhouse.ownedBy in release secret", Ordered, func() {
		const releaseName = "webhook-release-secret"

		It("should install the chart with global.greenhouse.ownedBy set", func() {
			helmInstall(releaseName, "--set", "global.greenhouse.ownedBy=secret-team")
		})

		It("should apply labels from the release secret on CREATE", func() {
			cm := getChartConfigMap(releaseName)

			Expect(cm.Labels).To(HaveKeyWithValue(supportGroupLabel, "secret-team"))
			Expect(cm.Labels).To(HaveKeyWithValue(serviceLabel, releaseName))
			Expect(cm.Annotations).To(HaveKeyWithValue(datasourceAnnotation, datasourceHelmSecret))
		})

		It("should have the release secret with the ownedBy value", func() {
			releaseSecretName := fmt.Sprintf("sh.helm.release.v1.%s.v1", releaseName)
			secret := &corev1.Secret{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      releaseSecretName,
				Namespace: namespace,
			}, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Data).To(HaveKey("release"))
		})
	})

	Context("helm install with owner ConfigMap fallback (no ownedBy in release)", Ordered, func() {
		const releaseName = "webhook-configmap-fallback"

		It("should pre-create the owner ConfigMap", func() {
			createOwnerConfigMap(releaseName, "cm-team", "cm-service")
		})

		It("should install the chart WITHOUT global.greenhouse.ownedBy", func() {
			helmInstall(releaseName)
		})

		It("should apply labels from the owner ConfigMap fallback", func() {
			cm := getChartConfigMap(releaseName)

			Expect(cm.Labels).To(HaveKeyWithValue(supportGroupLabel, "cm-team"))
			Expect(cm.Labels).To(HaveKeyWithValue(serviceLabel, "cm-service"))
			Expect(cm.Annotations).To(HaveKeyWithValue(datasourceAnnotation, datasourceOwnerInfo))
		})
	})

	Context("helm install without any owner source", Ordered, func() {
		const releaseName = "webhook-no-source"

		It("should install the chart without ownedBy and without owner ConfigMap", func() {
			helmInstall(releaseName)
		})

		It("should NOT apply owner labels", func() {
			cm := getChartConfigMap(releaseName)

			Expect(cm.Labels).NotTo(HaveKey(supportGroupLabel))
			Expect(cm.Labels).NotTo(HaveKey(serviceLabel))
		})
	})

	Context("label stickiness on upgrade", Ordered, func() {
		const releaseName = "webhook-stickiness"

		It("should pre-create the owner ConfigMap", func() {
			createOwnerConfigMap(releaseName, "cm-team", "cm-service")
		})

		It("should install WITHOUT ownedBy so labels come from ConfigMap", func() {
			helmInstall(releaseName)

			cm := getChartConfigMap(releaseName)
			Expect(cm.Labels).To(HaveKeyWithValue(supportGroupLabel, "cm-team"))
			Expect(cm.Annotations).To(HaveKeyWithValue(datasourceAnnotation, datasourceOwnerInfo))
		})

		It("should NOT change labels on upgrade even when ownedBy is added to the release", func() {
			helmUpgrade(releaseName,
				"--set", "global.greenhouse.ownedBy=secret-team",
				"--set", "triggerUpdate=true",
			)

			cm := getChartConfigMap(releaseName)

			Expect(cm.Labels).To(HaveKeyWithValue(supportGroupLabel, "cm-team"))
			Expect(cm.Labels).NotTo(HaveKeyWithValue(supportGroupLabel, "secret-team"))
		})
	})

	Context("release secret takes precedence over ConfigMap (no timing gap)", Ordered, func() {
		const releaseName = "webhook-precedence"

		It("should pre-create the owner ConfigMap with cm-team", func() {
			createOwnerConfigMap(releaseName, "cm-team", "cm-service")
		})

		It("should install the chart with global.greenhouse.ownedBy=secret-team", func() {
			helmInstall(releaseName, "--set", "global.greenhouse.ownedBy=secret-team")
		})

		It("should use the release secret (not the ConfigMap) proving no timing gap", func() {
			cm := getChartConfigMap(releaseName)

			Expect(cm.Labels).To(HaveKeyWithValue(supportGroupLabel, "secret-team"))
			Expect(cm.Annotations).To(HaveKeyWithValue(datasourceAnnotation, datasourceHelmSecret))
		})
	})

	Context("owner reference traversal (Pod -> ReplicaSet -> Deployment)", Ordered, func() {
		const releaseName = "webhook-owner-refs"
		const deploymentName = releaseName + "-app"

		It("should install the chart with a Deployment and global.greenhouse.ownedBy set", func() {
			helmInstall(releaseName,
				"--set", "global.greenhouse.ownedBy=ownerref-team",
				"--set", "deployment.enabled=true",
			)
		})

		It("should apply labels to the Deployment from the release secret", func() {
			deploy := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      deploymentName,
				Namespace: namespace,
			}, deploy)
			Expect(err).NotTo(HaveOccurred())

			Expect(deploy.Labels).To(HaveKeyWithValue(supportGroupLabel, "ownerref-team"))
			Expect(deploy.Annotations).To(HaveKeyWithValue(datasourceAnnotation, datasourceHelmSecret))
		})

		It("should apply labels to the ReplicaSet via owner reference traversal", func() {
			Eventually(func(g Gomega) {
				rsList := &appsv1.ReplicaSetList{}
				g.Expect(k8sClient.List(ctx, rsList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": deploymentName},
				)).To(Succeed())
				g.Expect(rsList.Items).NotTo(BeEmpty())

				rs := &rsList.Items[0]
				g.Expect(rs.Labels).To(HaveKeyWithValue(supportGroupLabel, "ownerref-team"))
			}).Should(Succeed())
		})

		It("should apply labels to the Pod via owner reference traversal", func() {
			Eventually(func(g Gomega) {
				podList := &corev1.PodList{}
				g.Expect(k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": deploymentName},
				)).To(Succeed())
				g.Expect(podList.Items).NotTo(BeEmpty())

				pod := &podList.Items[0]
				g.Expect(pod.Labels).To(HaveKeyWithValue(supportGroupLabel, "ownerref-team"))
			}).Should(Succeed())
		})
	})
})
