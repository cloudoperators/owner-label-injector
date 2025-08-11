// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"fmt"
	"os"

	"github.com/sapcc/go-bits/regexpext"
	"gopkg.in/yaml.v2"
)

type OwnerLabelInjectorConfig struct {
	Rules []Rule `yaml:"rules"`
}

type Rule struct {
	HelmReleaseName      regexpext.PlainRegexp `yaml:"helmReleaseName"`
	HelmReleaseNamespace regexpext.PlainRegexp `yaml:"helmReleaseNamespace"`

	SupportGroup string `yaml:"supportGroup"`
	Service      string `yaml:"service"`

	// TODO add resource name for a specific resource in a namespace/helm release
}

func (r *Rule) Validate() error {
	if r.SupportGroup == "" {
		return fmt.Errorf("empty support group for %s", r.HelmReleaseName)
	}

	return nil
}

const OwnerConfigRulesDatasource = "static-config"

func (r *OwnerLabelInjectorConfig) Check(helmReleaseName, helmReleaseNamespace string) (bool, OwnerData) {
	for _, rule := range r.Rules {
		if !rule.HelmReleaseName.MatchString(helmReleaseName) {
			continue
		}
		if !rule.HelmReleaseNamespace.MatchString(helmReleaseNamespace) {
			continue
		}

		return true, OwnerData{Service: rule.Service, SupportGroup: rule.SupportGroup, DataSource: OwnerConfigRulesDatasource}
	}

	return false, OwnerData{}
}

func NewConfig(configPath string) (*OwnerLabelInjectorConfig, error) {
	config := &OwnerLabelInjectorConfig{}

	// Open config file
	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Decode
	if err := yaml.NewDecoder(file).Decode(&config); err != nil {
		return nil, err
	}

	// Validate config rules
	for _, rule := range config.Rules {
		err := rule.Validate()
		if err != nil {
			return nil, err
		}
	}

	return config, nil
}
