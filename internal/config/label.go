// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package config

// LabelConfig defines the configurable label and annotation keys used by the owner-label-injector.
type Labels struct {
	// Prefix is the prefix used for all injected labels.
	Prefix string

	// SupportGroupSuffix is the suffix used for the support group label key.
	SupportGroupSuffix string

	// ServiceSuffix is the suffix used for the service label key.
	ServiceSuffix string

	// DataSourceAnnotationSuffix is the full annotation key used to track the data source.
	DataSourceAnnotationSuffix string
}

const (
	defaultPrefix                     = "ccloud"
	defaultSupportGroupSuffix         = "support-group"
	defaultServiceSuffix              = "service"
	defaultDataSourceAnnotationSuffix = "support-group-datasource"
)

// SupportGroupKey returns the full label key for support group.
func (c *Labels) SupportGroupKey() string {
	return c.Prefix + "/" + c.SupportGroupSuffix
}

// ServiceKey returns the full label key for service.
func (c *Labels) ServiceKey() string {
	return c.Prefix + "/" + c.ServiceSuffix
}

// DataSourceAnnotation returns the full annotation key for data source.
func (c *Labels) DataSourceAnnotation() string {
	return c.Prefix + "/" + c.DataSourceAnnotationSuffix
}
