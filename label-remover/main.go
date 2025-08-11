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

	jsonpatch "gomodules.xyz/jsonpatch/v2"

	"github.com/jedib0t/go-pretty/v6/list"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/pkg/errors"
	"github.com/sapcc/go-bits/httpext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"

	v1 "github.com/cloudoperators/owner-label-injector/api/v1"

	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

type APIs struct {
	NamespacedAPIs []schema.GroupVersionResource
	ClusterAPIs    []schema.GroupVersionResource
}

var EnableNamespacedAPIs bool
var EnableClusterLevelAPIs bool
var NamespacesFlag string
var SupportGroupToBeRemoved string
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
	flag.StringVar(&SupportGroupToBeRemoved, "support-group", "", "support group label value to be removed, if empty all support groups are removed")
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

	cfg := ctrl.GetConfigOrDie()
	dynamicClient := dynamic.NewForConfigOrDie(cfg)

	// config print
	fmt.Println(strings.Repeat("=", dashLength))
	fmt.Println("Configuration")
	fmt.Println(strings.Repeat("=", dashLength))
	fmt.Println("Cluster level APIs:		", EnableClusterLevelAPIs)
	fmt.Println("Namespaced APIs:		", EnableNamespacedAPIs)
	fmt.Println("Support group to be removed:	", SupportGroupToBeRemoved)

	fmt.Println("Dry run:			", DryRun)
	fmt.Println()

	if NamespacesFlag == "all" {
		fmt.Println("Namespaces:		 	all")
	} else {
		fmt.Println("Namespaces: 			", NamespacesListFromFlag)
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

			handleResources(ctx, dynamicClient, capi, "")

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
					handleResources(ctx, dynamicClient, api, namespace)
				}
			} else {
				for _, namespace := range NamespacesListFromFlag {
					handleResources(ctx, dynamicClient, api, namespace)
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
	tw.AppendHeader(table.Row{"Resource", "With Specific Label", "Total"})

	for _, capi := range apis.ClusterAPIs {
		gvr := capi.GroupVersion().WithResource(capi.Resource)

		totalResources, err := GetResourcesDynamically(ctx, dynamicInt, gvr, "")
		if err != nil {
			continue
		}

		resourcesWOLabels, err := GetResourcesWithSpecificLabel(ctx, dynamicInt, gvr, "")
		if err != nil {
			continue
		}

		tw.AppendRow(table.Row{gvr.String(), len(resourcesWOLabels), len(totalResources)})
	}

	fmt.Println(tw.Render())

	fmt.Println(strings.Repeat("=", dashLength))
	fmt.Println("Namespaced APIs (all namespaces)")
	fmt.Println(strings.Repeat("=", dashLength))

	twNamespaced := table.NewWriter()
	twNamespaced.AppendHeader(table.Row{"Resource", "With Specific Label", "Total"})

	for _, napi := range apis.NamespacedAPIs {
		gvr := napi.GroupVersion().WithResource(napi.Resource)

		totalResources, err := GetResourcesDynamically(ctx, dynamicInt, gvr, "")
		if err != nil {
			continue
		}

		resourcesWOLabels, err := GetResourcesWithSpecificLabel(ctx, dynamicInt, gvr, "")
		if err != nil {
			continue
		}

		twNamespaced.AppendRow(table.Row{gvr.String(), len(resourcesWOLabels), len(totalResources)})
	}

	fmt.Println(twNamespaced.Render())
}

func handleResources(ctx context.Context, dynamicInt dynamic.Interface, gvr schema.GroupVersionResource, namespace string) {
	resources, err := GetResourcesWithSpecificLabel(ctx, dynamicInt, gvr, namespace)
	if err != nil {
		fmt.Println(err)
		return
	}

	if len(resources) > 0 {
		if namespace != "" {
			fmt.Printf("[namespace: %s] %d resources found (with specific label)\n", namespace, len(resources))
		} else {
			fmt.Printf("%d resources found with specific label\n", len(resources))
		}
	}

	for r, resource := range resources {
		logHeader := fmt.Sprintf("- [%d/%d %s]", r+1, len(resources), resource.GetName())

		// Copy object
		newResource := unstructured.Unstructured{}
		resource.DeepCopyInto(&newResource)

		// clean labels
		currentLabels := newResource.GetLabels()
		fmt.Printf("%s labels before: %+v \n", logHeader, currentLabels)
		// delete(currentLabels, v1.LABEL_SERVICE)
		delete(currentLabels, v1.LabelSupportGroup)
		newResource.SetLabels(currentLabels)
		fmt.Printf("%s labels after: %+v \n", logHeader, currentLabels)

		// clean annotation
		currentAnnotations := resource.GetAnnotations()
		delete(currentAnnotations, v1.AnnotationSupportGroupDataSource)
		newResource.SetAnnotations(currentAnnotations)

		if DryRun {
			continue
		}
		// patch data
		patchData, err := getPatchData(resource.Object, newResource.Object)
		if err != nil {
			fmt.Printf("%s error during patch data: %v \n", logHeader, err)
			continue
		}

		_, err = dynamicInt.Resource(gvr).Namespace(namespace).Patch(ctx, resource.GetName(), types.JSONPatchType, patchData, metav1.PatchOptions{})
		if err != nil {
			fmt.Printf("%s error during patch: %v \n", logHeader, err)
			continue
		}
		fmt.Printf("%s labels patched! \n", logHeader)
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

func GetResourcesWithSpecificLabel(ctx context.Context, dynamicInt dynamic.Interface, gvr schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, error) {
	listOptions := metav1.ListOptions{}
	if SupportGroupToBeRemoved != "" {
		labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{v1.LabelSupportGroup: SupportGroupToBeRemoved}}
		listOptions.LabelSelector = labels.Set(labelSelector.MatchLabels).String()
	}

	labelList, err := dynamicInt.Resource(gvr).Namespace(namespace).List(ctx, listOptions)
	if err != nil {
		return nil, err
	}

	return labelList.Items, nil
}

func getPatchData(originalObj, modifiedObj any) ([]byte, error) {
	originalData, err := json.Marshal(originalObj)
	if err != nil {
		return nil, errors.Wrapf(err, "failed marshal original data")
	}
	modifiedData, err := json.Marshal(modifiedObj)
	if err != nil {
		return nil, errors.Wrapf(err, "failed marshal modified data")
	}

	patchData, err := jsonpatch.CreatePatch(originalData, modifiedData)
	if err != nil {
		return nil, errors.Errorf("create jsonpatch failed: %v", err)
	}

	patchBytes, err := json.Marshal(patchData)
	if err != nil {
		return nil, errors.Wrapf(err, "failed marshal jsonpatch data")
	}

	return patchBytes, nil
}
