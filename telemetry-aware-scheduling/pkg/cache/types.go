// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"time"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	telemetrypolicy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
)

// Reader is the functionality to read metrics and policies from the cache.
type Reader interface {
	ReadMetric(metricName string) (metrics.NodeMetricsInfo, error)
	ReadPolicy(podNamespace string, policyName string) (telemetrypolicy.TASPolicy, error)
}

// Writer is the functionality to edit metrics (write and delete) and Policies in the cache.
type Writer interface {
	WriteMetric(metricName string, metricInfo metrics.NodeMetricsInfo) error
	WritePolicy(policyNamespace string, policyName string, policy telemetrypolicy.TASPolicy) error
	DeleteMetric(metricName string) error
	DeletePolicy(policyNamespace string, policyName string) error
}

// ReaderWriter holds the functionality to both read and write metrics and policies.
type ReaderWriter interface {
	Reader
	Writer
}

// SelfUpdating describes functionality to periodically update all metrics in the metric cache.
type SelfUpdating interface {
	PeriodicUpdate(metricTicker time.Ticker, metricsClient metrics.Client, data map[string]interface{})
}
