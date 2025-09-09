// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cloudoperators/owner-label-injector/internal/config"
)

// +kubebuilder:webhook:path=/mutate-generic,mutating=true,failurePolicy=ignore,groups="*",resources="*",verbs=create;update,sideEffects=NoneOnDryRun,versions="*",admissionReviewVersions=v1,name=owner-label-injector.generic.ccloud
// +kubebuilder:rbac:groups="*",resources="*",verbs=list;get;patch

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=list;get;watch

type GenericLabeller struct {
	Config  *config.Global
	Client  client.Client
	Decoder admission.Decoder
	Logger  logr.Logger
}

func (a *GenericLabeller) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := a.Logger.WithValues("name", req.Name, "namespace", req.Namespace)

	object := &unstructured.Unstructured{}
	err := a.Decoder.Decode(req, object)
	if err != nil {
		log.Error(err, "error during decode")
		return admission.Errored(http.StatusBadRequest, errors.Wrap(err, "Error during decoding the incoming object"))
	}

	currentOwnerDataFound, currentOwnerData := GetOwnerDataFromLabels(object.GetLabels(), a.Config)

	ownerData, found, err := UpwardTraverseGetOwnerData(ctx, a.Client, a.Config, req.Namespace, object, false)
	if err != nil {
		log.Error(err, "error during get owner data")
		return admission.Allowed(fmt.Sprintf("No owner-labels are injected. Error occurred during get owner data: %v", err))
	}
	if !found {
		return admission.Allowed("No owner-labels are injected. Owner data is not found.")
	}

	currentLabels := object.GetLabels()
	if currentLabels == nil {
		currentLabels = make(map[string]string, 0)
	}
	maps.Copy(currentLabels, ownerData.Labels(a.Config))
	object.SetLabels(currentLabels)

	// Check for generated resources like pods in workload API
	object, workloadAPIChanged, err := WorkloadAPILabeller(object, ownerData, a.Config)
	if err != nil {
		log.Error(err, "error during workload API labelling")
		return admission.Allowed(fmt.Sprintf("No owner-labels are injected. Error occurred workload API labelling: %v", err))
	}

	// compare owner data and change in workload API
	if currentOwnerDataFound && currentOwnerData.CompareWithoutDataSource(ownerData) {
		if !workloadAPIChanged {
			return admission.Allowed("Owner labels are already attached correctly and no workload API change required")
		}
		log.Info("Owner labels are already attached correctly but workload API change is required")
	}

	// Set annotation for the datasource
	currentAnnotations := object.GetAnnotations()
	if currentAnnotations != nil {
		currentAnnotations[a.Config.Labels.DataSourceAnnotation()] = ownerData.DataSource
	} else {
		currentAnnotations = map[string]string{a.Config.Labels.DataSourceAnnotation(): ownerData.DataSource}
	}
	object.SetAnnotations(currentAnnotations)

	marshaledPod, err := json.Marshal(object)
	if err != nil {
		log.Error(err, "error during marshall")
		return admission.Allowed(fmt.Sprintf("No owner-labels are injected. Error occurred while marshalling: %v", err))
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

type OwnerData struct {
	SupportGroup string
	Service      string
	DataSource   string
}

func (o OwnerData) Labels(cfg *config.Global) map[string]string {
	labels := make(map[string]string, 0)

	if o.SupportGroup != "" {
		labels[cfg.Labels.SupportGroupKey()] = o.SupportGroup
	}

	if o.Service != "" {
		labels[cfg.Labels.ServiceKey()] = o.Service
	}

	return labels
}

func (o OwnerData) CompareWithoutDataSource(c OwnerData) bool {
	return o.Service == c.Service && o.SupportGroup == c.SupportGroup
}
