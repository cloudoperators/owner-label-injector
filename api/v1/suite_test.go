// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/cloudoperators/owner-label-injector/api/v1"
	"github.com/cloudoperators/owner-label-injector/internal/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

var (
	testEnv        *envtest.Environment
	k8sClient      client.Client
	k8sManager     ctrl.Manager
	stopController context.CancelFunc
	testConfig     *config.Global
)

func TestGenericLabeller(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Generic Labeller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{}

	webhookURL := "https://127.0.0.1:9443/mutate-generic"
	scope := admissionregistrationv1.NamespacedScope
	sideEffects := admissionregistrationv1.SideEffectClassNone
	mutatingWebhookCfg := &admissionregistrationv1.MutatingWebhookConfiguration{}
	mutatingWebhookCfg.Name = "owner-label-injector.cloud.sap"
	mutatingWebhookCfg.Webhooks = []admissionregistrationv1.MutatingWebhook{
		{
			Name: "owner-label-injector.cloud.sap",
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				URL: &webhookURL,
			},
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create, admissionregistrationv1.Update,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"", "apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"*"},
						Scope:       &scope,
					},
				},
			},
			AdmissionReviewVersions: []string{"v1"},
			SideEffects:             &sideEffects,
		},
	}
	testEnv.WebhookInstallOptions.MutatingWebhooks = []*admissionregistrationv1.MutatingWebhookConfiguration{
		mutatingWebhookCfg,
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Logger: GinkgoLogr,
		WebhookServer: webhook.NewServer(webhook.Options{
			CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
		}),
	})
	Expect(err).ToNot(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	rule := v1.Rule{
		HelmReleaseNamespace: metav1.NamespaceDefault,
		HelmReleaseName:      "static-release",
		SupportGroup:         "static-group",
		Service:              "static-service",
	}
	Expect(rule.Validate()).To(Succeed())

	testConfig = &config.Global{
		Labels: config.Labels{
			Prefix:                     "ccloud",
			SupportGroupSuffix:         "support-group",
			ServiceSuffix:              "service",
			DataSourceAnnotationSuffix: "support-group-datasource",
		},
		Helm: config.Helm{
			OwnerConfigMapPrefix:         "owner-of-",
			OwnerConfigMapFallbackPrefix: "early-owner-of-",
			SupportGroupDataKey:          "supportGroup",
			ServiceDataKey:               "service",
		},
		StaticRules: config.StaticRules{
			Rules: []config.StaticRule{
				{
					HelmReleaseName:      "static-release",
					HelmReleaseNamespace: metav1.NamespaceDefault,
					SupportGroup:         "static-group",
					Service:              "static-service",
				},
			},
		},
		Traversal: config.Traversal{
			VicePresidentAnnotationKey: "vice-president/claimed-by-ingress",
		},
	}

	labeller := v1.GenericLabeller{
		Config:  testConfig,
		Client:  k8sClient,
		Logger:  GinkgoLogr,
		Decoder: admission.NewDecoder(scheme.Scheme),
	}
	k8sManager.GetWebhookServer().Register("/mutate-generic", &webhook.Admission{Handler: &labeller})

	go func() {
		defer GinkgoRecover()
		stopCtx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
		stopController = cancel
		err = k8sManager.Start(stopCtx)
		Expect(err).ToNot(HaveOccurred())
	}()

	// The default namespace needs some time to be created...
	time.Sleep(20 * time.Millisecond)
})

var _ = AfterSuite(func() {
	stopController()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
