// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2024-present Datadog, Inc.

// Package provider TBD
package provider

import (
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/otelcol"
)

// team: opentelemetry

// Component implements the otelcol.ConfigProvider interface and
// provides extra functions to expose the provided and enhanced configs.
type Component interface {
	confmap.Converter
	otelcol.ConfigProvider
	GetProvidedConf() string
	GetEnhancedConf() string
}

// Requires TBD
type Requires struct {
	URIs []string
}
