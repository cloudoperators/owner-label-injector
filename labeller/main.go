// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch"

	"github.com/jedib0t/go-pretty/v6/list"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/pkg/errors"
	"github.com/sapcc/go-bits/httpext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	apiV1 "github.com/cloudoperators/owner-label-injector/api/v1"
	internalCfg "github.com/cloudoperators/owner-label-injector/internal/config"

	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

type APIs struct {
	NamespacedAPIs []schema.GroupVersionResource
	ClusterAPIs    []schema.GroupVersionResource
}

var EnableNamespacedAPIs bool
var EnableClusterLevelAPIs bool
var NamespacesFlag string
var Summary bool
var NamespacesListFromFlag []string = make([]string, 0)
var DryRun bool

func Discover(ctx context.Context, c *discovery.DiscoveryClient, d dynamic.Interface) (APIs, []string, error) {
	apis := APIs{
		NamespacedAPIs: make([]schema.GroupVersionResource, 0),
		ClusterAPIs:    make([]schema.GroupVersionResource, 0),
	}

	items, err := c.ServerPreferredResources()
	if err != nil {
		return apis, nil, err
	}

	for _, item := range items {
		gv, err := schema.ParseGroupVersion(item.GroupVersion)
		if err != nil {
			return apis, nil, err
		}

		for _, apiResource := range item.APIResources {
			if apiResource.Namespaced {
				apis.NamespacedAPIs = append(apis.NamespacedAPIs, gv.WithResource(apiResource.Name))
			} else {
				apis.ClusterAPIs = append(apis.ClusterAPIs, gv.WithResource(apiResource.Name))
			}
		}
	}

	namespaceObjects, err := GetResourcesDynamically(ctx, d, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, "")
	if err != nil {
		return apis, nil, err
	}
	namespaces := make([]string, 0)
	for _, namespaceObject := range namespaceObjects {
		namespaces = append(namespaces, namespaceObject.GetName())
	}

	return apis, namespaces, nil
}

func main() {
	flag.StringVar(&NamespacesFlag, "namespace", "all", "namespaces to run, pass a comma-separated list for more than one namespace")
	flag.BoolVar(&EnableClusterLevelAPIs, "cluster-level-apis", true, "run for cluster-level apis")
	flag.BoolVar(&EnableNamespacedAPIs, "namespaced-apis", true, "run for namespaced apis")
	flag.BoolVar(&Summary, "summary", false, "only print summary")
	flag.BoolVar(&DryRun, "dry-run", false, "do not label resources on the cluster")

	flag.Parse()

	ctx := httpext.ContextWithSIGINT(context.Background(), 10*time.Second)

	// config validation
	if NamespacesFlag != "all" {
		if NamespacesFlag == "" {
			fmt.Println("empty namespace is not supported")
			return
		}
		NamespacesListFromFlag = strings.Split(NamespacesFlag, ",")
		if len(NamespacesListFromFlag) == 0 {
			fmt.Println("namespace splitting failed")
			return
		}
	}

	cfg := config.GetConfigOrDie()
	dynamicClient := dynamic.NewForConfigOrDie(cfg)
	k8sClient, err := client.New(cfg, client.Options{})
	if err != nil {
		panic(err)
	}

	globalConfig := internalCfg.Get()
	// config print
	fmt.Println(strings.Repeat("=", dashLength))
	fmt.Println("Configuration")
	fmt.Println(strings.Repeat("=", dashLength))
	fmt.Println("Cluster level APIs:	", EnableClusterLevelAPIs)
	fmt.Println("Namespaced APIs:	", EnableNamespacedAPIs)
	fmt.Println("Dry run:		", DryRun)
	fmt.Println()
	fmt.Println("Config from env vars:")
	fmt.Printf("  Support Group Label:\t%s\n", globalConfig.Labels.SupportGroupKey())
	fmt.Printf("  Service Label:\t%s\n", globalConfig.Labels.ServiceKey())
	fmt.Printf("  Data Source Annotation:\t%s\n", globalConfig.Labels.DataSourceAnnotation())
	fmt.Printf("  Static Rules:\t\t%d configured\n", len(globalConfig.StaticRules.Rules))

	if NamespacesFlag == "all" {
		fmt.Println("Namespaces:		 all")
	} else {
		fmt.Println("Namespaces: 		", NamespacesListFromFlag)
	}

	fmt.Println()
	fmt.Println("Kubernetes Endpoint:	", cfg.Host)
	fmt.Println(strings.Repeat("=", dashLength))

	disco := discovery.NewDiscoveryClientForConfigOrDie(cfg)

	// Discovery
	apis, namespaces, err := Discover(ctx, disco, dynamicClient)
	if err != nil {
		panic(err)
	}

	if Summary {
		summary(ctx, apis, dynamicClient)
		return
	}
	printDiscoveryResult(apis, namespaces)

	if EnableClusterLevelAPIs {
		fmt.Println(strings.Repeat("=", dashLength))
		fmt.Println("Cluster-level APIs")
		fmt.Println(strings.Repeat("=", dashLength))
		for i, capi := range apis.ClusterAPIs {
			fmt.Println(strings.Repeat("=", dashLength))
			fmt.Printf("[%d/%d] %s/%s\n", i+1, len(apis.ClusterAPIs), capi.GroupVersion(), capi.Resource)

			handleResources(ctx, dynamicClient, k8sClient, globalConfig, capi, "")

			fmt.Println(strings.Repeat("=", dashLength))
		}
	}

	if EnableNamespacedAPIs {
		fmt.Println(strings.Repeat("=", dashLength))
		fmt.Println("Namespaced APIs")
		fmt.Println(strings.Repeat("=", dashLength))
		for i, api := range apis.NamespacedAPIs {
			fmt.Println(strings.Repeat("=", dashLength))

			fmt.Printf("[%d/%d] %s/%s\n", i+1, len(apis.NamespacedAPIs), api.GroupVersion(), api.Resource)

			if NamespacesFlag == "all" {
				for _, namespace := range namespaces {
					handleResources(ctx, dynamicClient, k8sClient, globalConfig, api, namespace)
				}
			} else {
				for _, namespace := range NamespacesListFromFlag {
					handleResources(ctx, dynamicClient, k8sClient, globalConfig, api, namespace)
				}
			}

			fmt.Println(strings.Repeat("=", dashLength))
		}
	}

	summary(ctx, apis, dynamicClient)
}

func summary(ctx context.Context, apis APIs, dynamicInt dynamic.Interface) {
	fmt.Println(strings.Repeat("=", dashLength))
	fmt.Println("SUMMARY")
	fmt.Println(strings.Repeat("=", dashLength))

	fmt.Println(strings.Repeat("=", dashLength))
	fmt.Println("Cluster-level APIs")
	fmt.Println(strings.Repeat("=", dashLength))

	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"Resource", "Without Label", "Total"})

	for _, capi := range apis.ClusterAPIs {
		gvr := capi.GroupVersion().WithResource(capi.Resource)

		totalResources, err := GetResourcesDynamically(ctx, dynamicInt, gvr, "")
		if err != nil {
			continue
		}
		tw.AppendRow(table.Row{gvr.String(), countWithoutOwnerLabels(totalResources), len(totalResources)})
	}

	fmt.Println(tw.Render())

	fmt.Println(strings.Repeat("=", dashLength))
	fmt.Println("Namespaced APIs (all namespaces)")
	fmt.Println(strings.Repeat("=", dashLength))

	twNamespaced := table.NewWriter()
	twNamespaced.AppendHeader(table.Row{"Resource", "Without Label", "Total"})

	for _, napi := range apis.NamespacedAPIs {
		gvr := napi.GroupVersion().WithResource(napi.Resource)
		totalResources, err := GetResourcesDynamically(ctx, dynamicInt, gvr, "")
		if err != nil {
			continue
		}
		twNamespaced.AppendRow(table.Row{gvr.String(), countWithoutOwnerLabels(totalResources), len(totalResources)})
	}

	fmt.Println(twNamespaced.Render())
}

func countWithoutOwnerLabels(resources []unstructured.Unstructured) int {
	count := 0
	globalConfig := internalCfg.Get()
	for _, resource := range resources {
		labels := resource.GetLabels()
		if labels == nil {
			count++
			continue
		}
		_, hasGroup := labels[globalConfig.Labels.SupportGroupKey()]
		_, hasService := labels[globalConfig.Labels.ServiceKey()]
		if !hasGroup || !hasService {
			count++
		}
	}
	return count
}

func handleResources(ctx context.Context, dynamicInt dynamic.Interface, k8sClient client.Client, globalConfig *internalCfg.Global, gvr schema.GroupVersionResource, namespace string) {
	resources, err := GetResourcesDynamically(ctx, dynamicInt, gvr, namespace)
	if err != nil {
		fmt.Println(err)
		return
	}

	if len(resources) > 0 {
		if namespace != "" {
			fmt.Printf("[namespace: %s] %d resources found\n", namespace, len(resources))
		} else {
			fmt.Printf("%d resources found\n", len(resources))
		}
	}

	for r, resource := range resources {
		logHeader := fmt.Sprintf("- [%d/%d %s]", r+1, len(resources), resource.GetName())

		ownerData, found, err := apiV1.UpwardTraverseGetOwnerData(ctx, k8sClient, globalConfig, namespace, &resource, true)
		if err != nil {
			fmt.Printf("%s Error during get owner data: %v \n", logHeader, err)
			continue
		}
		if !found {
			fmt.Printf("%s OwnerData not found, skipping\n", logHeader)
			continue
		}
		fmt.Printf("%s OwnerData is found: %+v \n", logHeader, ownerData)

		// Get current annotations
		currentAnnotations := resource.GetAnnotations()
		if currentAnnotations == nil {
			currentAnnotations = make(map[string]string)
		}
		existingDataSource, hasExistingDataSource := currentAnnotations[globalConfig.Labels.DataSourceAnnotation()]

		// Only overwrite labels if DataSource is non‐empty and different
		if hasExistingDataSource && existingDataSource != "" && existingDataSource != ownerData.DataSource {
			fmt.Printf("%s DataSource is different (existing: %s, new: %s), skipping label update\n", logHeader, existingDataSource, ownerData.DataSource)
			continue
		}

		// Copy object
		newResource := unstructured.Unstructured{}
		resource.DeepCopyInto(&newResource)

		// Get current labels
		currentLabels := newResource.GetLabels()
		if currentLabels == nil {
			currentLabels = make(map[string]string)
		}
		fmt.Printf("%s Labels before: %+v \n", logHeader, currentLabels)

		// Update labels by comparing with ownerData
		labelsUpdated := false
		for k, v := range ownerData.Labels(globalConfig) {
			currentValue, exists := currentLabels[k]
			if !exists || currentValue != v {
				currentLabels[k] = v
				labelsUpdated = true
			}
		}

		if !labelsUpdated {
			fmt.Printf("%s Labels are up-to-date, skipping\n", logHeader)
			continue
		}

		newResource.SetLabels(currentLabels)
		fmt.Printf("%s Labels updated to: %+v \n", logHeader, currentLabels)

		// Update annotations
		currentAnnotations[globalConfig.Labels.DataSourceAnnotation()] = ownerData.DataSource
		newResource.SetAnnotations(currentAnnotations)

		if DryRun {
			fmt.Printf("%s Dry-run mode: Changes not applied\n", logHeader)
			continue
		}

		// Patch data
		patchData, err := getPatchData(&resource, &newResource)
		if err != nil {
			fmt.Printf("%s Error during patch data: %v \n", logHeader, err)
			continue
		}

		_, err = dynamicInt.Resource(gvr).Namespace(namespace).Patch(ctx, resource.GetName(), types.MergePatchType, patchData, metav1.PatchOptions{})
		if err != nil {
			fmt.Printf("%s Error during patch: %v \n", logHeader, err)
			continue
		}
		fmt.Printf("%s Labels patched! \n", logHeader)
	}
}

const dashLength = 50

func printDiscoveryResult(apis APIs, namespaces []string) {
	fmt.Println(strings.Repeat("=", dashLength))
	fmt.Println("Discovery Summary")
	fmt.Println(strings.Repeat("=", dashLength))
	fmt.Printf("%d Cluster-level APIs, %d Namespaced APIs \n", len(apis.ClusterAPIs), len(apis.NamespacedAPIs))
	fmt.Printf("%d Namespaces \n", len(namespaces))
	fmt.Println(strings.Repeat("=", dashLength))

	l := list.NewWriter()
	l.SetStyle(list.StyleConnectedRounded)
	l.AppendItem("APIs")
	l.Indent()
	l.AppendItem("Cluster-level APIs")
	l.Indent()
	for _, ca := range apis.ClusterAPIs {
		l.AppendItem(fmt.Sprintf("%s/%s", ca.GroupVersion(), ca.Resource))
	}
	l.UnIndent()
	l.AppendItem("Namespaced APIs")
	l.Indent()
	for _, ca := range apis.NamespacedAPIs {
		l.AppendItem(fmt.Sprintf("%s/%s", ca.GroupVersion(), ca.Resource))
	}
	l.UnIndent()
	l.UnIndent()
	l.AppendItem("Namespaces")
	l.Indent()
	for _, n := range namespaces {
		l.AppendItem(n)
	}
	fmt.Println(l.Render())
	fmt.Println(strings.Repeat("=", dashLength))
}

func GetResourcesDynamically(ctx context.Context, dynamicInt dynamic.Interface, gvr schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, error) {
	resourceList, err := dynamicInt.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})

	if err != nil {
		return nil, err
	}

	return resourceList.Items, nil
}

func getPatchData(original, modified *unstructured.Unstructured) ([]byte, error) {
	originalData, err := json.Marshal(original.Object)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal original data")
	}

	modifiedData, err := json.Marshal(modified.Object)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal modified data")
	}

	patchData, err := jsonpatch.CreateMergePatch(originalData, modifiedData)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create merge patch")
	}

	return patchData, nil
}
