// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"os"
	"sync"
)

// Config holds all configuration for the owner-label-injector.
type Global struct {
	Labels
	Helm
	StaticRules
	Traversal
}

var (
	globalConfig *Global
	configOnce   sync.Once
)

// Get returns the Global instance - initialized once from environment variables.
func Get() *Global {
	configOnce.Do(func() {
		labels := Labels{
			Prefix:                     getEnvOrDefault("LABEL_PREFIX", defaultPrefix),
			SupportGroupSuffix:         getEnvOrDefault("SUPPORT_GROUP_SUFFIX", defaultSupportGroupSuffix),
			ServiceSuffix:              getEnvOrDefault("SERVICE_SUFFIX", defaultServiceSuffix),
			DataSourceAnnotationSuffix: getEnvOrDefault("DATA_SOURCE_ANNOTATION_SUFFIX", defaultDataSourceAnnotationSuffix),
		}

		helm := Helm{
			OwnerConfigMapPrefix:         getEnvOrDefault("OWNER_CONFIGMAP_PREFIX", defaultOwnerConfigMapPrefix),
			OwnerConfigMapFallbackPrefix: getEnvOrDefault("OWNER_CONFIGMAP_FALLBACK_PREFIX", defaultOwnerConfigMapFallbackPrefix),
			SupportGroupDataKey:          getEnvOrDefault("SUPPORT_GROUP_DATA_KEY", defaultSupportGroupDataKey),
			ServiceDataKey:               getEnvOrDefault("SERVICE_DATA_KEY", defaultServiceDataKey),
		}

		staticRules := StaticRules{}
		rulesJSON := getEnvOrDefault("STATIC_RULES", "")
		if rulesJSON != "" {
			if err := json.Unmarshal([]byte(rulesJSON), &staticRules); err != nil {
				staticRules = StaticRules{Rules: []StaticRule{}}
			}
		}

		traversal := Traversal{
			VicePresidentAnnotationKey: getEnvOrDefault("VICE_PRESIDENT_ANNOTATION_KEY", defaultVicePresidentAnnotationKey),
		}

		globalConfig = &Global{
			Labels:      labels,
			Helm:        helm,
			StaticRules: staticRules,
			Traversal:   traversal,
		}
	})
	return globalConfig
}

// getEnvOrDefault is a helper function to get environment variable with fallback to default value.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
