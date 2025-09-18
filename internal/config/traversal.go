// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package config

// Traversal defines configurable traversal behavior and special cases.
type Traversal struct {
	// VicePresidentAnnotationKey is the annotation key for TLS cert ingress discovery.
	VicePresidentAnnotationKey string
}

const (
	defaultVicePresidentAnnotationKey = "vice-president/claimed-by-ingress"
)
