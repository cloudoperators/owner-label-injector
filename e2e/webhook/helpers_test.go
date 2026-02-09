//go:build webhookE2E

package webhook

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These must match the webhook's default label/annotation config.
const (
	supportGroupLabel    = "ccloud/support-group"
	serviceLabel         = "ccloud/service"
	datasourceAnnotation = "ccloud/support-group-datasource"

	datasourceHelmSecret = "helm-release-secret"
	datasourceOwnerInfo  = "owner-info"

	ownerConfigMapPrefix = "owner-of-"
	chartConfigMapSuffix = "-data"
)

var (
	k8sClient client.Client
	namespace string
	chartPath string
	ctx       = context.Background()

	// Track releases and owner ConfigMaps for cleanup.
	installedReleases   []string
	createdOwnerConfigs []string
)

func TestWebhookE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook E2E Suite")
}

var _ = BeforeSuite(func() {
	namespace = os.Getenv("E2E_NAMESPACE")
	if namespace == "" {
		namespace = "oli-e2e"
	}

	_, thisFile, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue())
	chartPath = filepath.Join(filepath.Dir(thisFile), "..", "test-chart-webhook")

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		Expect(err).NotTo(HaveOccurred())
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	ns := &corev1.Namespace{}
	err = k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, ns)
	Expect(err).NotTo(HaveOccurred(), "test namespace %s must exist - run setup.sh first", namespace)
})

var _ = AfterSuite(func() {
	for _, name := range installedReleases {
		helmDelete(name)
	}
	if k8sClient != nil {
		for _, name := range createdOwnerConfigs {
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			})
		}
	}
})

// helmInstall installs the test chart and registers the release for cleanup.
func helmInstall(releaseName string, extraArgs ...string) {
	installedReleases = append(installedReleases, releaseName)
	args := []string{
		"install", releaseName, chartPath,
		"--namespace", namespace,
		"--wait", "--timeout", "60s",
	}
	args = append(args, extraArgs...)
	cmd := exec.Command("helm", args...)
	output, err := cmd.CombinedOutput()
	GinkgoWriter.Printf("helm install %s:\n%s\n", releaseName, string(output))
	Expect(err).NotTo(HaveOccurred(), "helm install failed: %s", string(output))
}

// helmUpgrade upgrades the release.
func helmUpgrade(releaseName string, extraArgs ...string) {
	args := []string{
		"upgrade", releaseName, chartPath,
		"--namespace", namespace,
		"--wait", "--timeout", "60s",
	}
	args = append(args, extraArgs...)
	cmd := exec.Command("helm", args...)
	output, err := cmd.CombinedOutput()
	GinkgoWriter.Printf("helm upgrade %s:\n%s\n", releaseName, string(output))
	Expect(err).NotTo(HaveOccurred(), "helm upgrade failed: %s", string(output))
}

func helmDelete(releaseName string) {
	cmd := exec.Command("helm", "delete", releaseName, "--namespace", namespace)
	output, _ := cmd.CombinedOutput()
	GinkgoWriter.Printf("helm delete %s:\n%s\n", releaseName, string(output))
}

// createOwnerConfigMap creates an owner-of-<releaseName> ConfigMap and registers it for cleanup.
func createOwnerConfigMap(releaseName, supportGroup, service string) {
	name := ownerConfigMapPrefix + releaseName
	createdOwnerConfigs = append(createdOwnerConfigs, name)
	ownerCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"support-group": supportGroup,
			"service":       service,
		},
	}
	err := k8sClient.Create(ctx, ownerCM)
	Expect(err).NotTo(HaveOccurred())
}

// getChartConfigMap fetches the ConfigMap created by the test chart (<releaseName>-data).
func getChartConfigMap(releaseName string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      releaseName + chartConfigMapSuffix,
		Namespace: namespace,
	}, cm)
	Expect(err).NotTo(HaveOccurred())
	GinkgoWriter.Printf("ConfigMap %s labels: %v\n", cm.Name, cm.Labels)
	GinkgoWriter.Printf("ConfigMap %s annotations: %v\n", cm.Name, cm.Annotations)
	return cm
}
