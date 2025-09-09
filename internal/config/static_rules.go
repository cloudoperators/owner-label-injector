// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"github.com/sapcc/go-bits/regexpext"
)

// StaticRulesConfig manages static rules that can be provided via environment variables or plugin options.
type StaticRules struct {
	Rules []StaticRule `json:"rules"`
}

// StaticRule represents a single static rule for JSON serialization.
type StaticRule struct {
	HelmReleaseName      string `json:"helmReleaseName"`
	HelmReleaseNamespace string `json:"helmReleaseNamespace"`
	SupportGroup         string `json:"supportGroup"`
	Service              string `json:"service"`
}

const staticConfigDataSource = "static-config"

// Check looks for a matching rule for the given Helm release name and namespace.
func (c *StaticRules) Check(helmReleaseName, helmReleaseNamespace string) (bool, StaticRuleMatch) {
	for _, rule := range c.Rules {
		releaseNameRegex := regexpext.PlainRegexp(rule.HelmReleaseName)
		releaseNamespaceRegex := regexpext.PlainRegexp(rule.HelmReleaseNamespace)

		if !releaseNameRegex.MatchString(helmReleaseName) {
			continue
		}
		if !releaseNamespaceRegex.MatchString(helmReleaseNamespace) {
			continue
		}

		return true, StaticRuleMatch{
			Service:      rule.Service,
			SupportGroup: rule.SupportGroup,
			DataSource:   staticConfigDataSource,
		}
	}

	return false, StaticRuleMatch{}
}

// StaticRuleMatch represents the result of a static rule match.
type StaticRuleMatch struct {
	Service      string
	SupportGroup string
	DataSource   string
}
