// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package config

// HelmConfig defines the configurable Helm-related settings for ConfigMap discovery.
type Helm struct {
	// OwnerConfigMapPrefix is the prefix for primary owner ConfigMaps.
	OwnerConfigMapPrefix string

	// OwnerConfigMapFallbackPrefix is the prefix for fallback owner ConfigMaps.
	OwnerConfigMapFallbackPrefix string

	// SupportGroupDataKey is the key used in ConfigMaps for support group data.
	SupportGroupDataKey string

	// ServiceDataKey is the key used in ConfigMaps for service data.
	ServiceDataKey string
}

const (
	defaultOwnerConfigMapPrefix         = "owner-of-"
	defaultOwnerConfigMapFallbackPrefix = "early-owner-of-"
	defaultSupportGroupDataKey          = "support-group"
	defaultServiceDataKey               = "service"
)
