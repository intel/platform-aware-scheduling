// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package metrics instruments to read and cache Node Metrics from the custom metrics API.
package metrics

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	cacheddiscovery "k8s.io/client-go/discovery/cached"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/metrics/pkg/apis/custom_metrics/v1beta2"
	customclient "k8s.io/metrics/pkg/client/custom_metrics"
)

var errNull = errors.New("")

// Client knows how to query CustomMetricsAPI to return Node Metrics.
type Client interface {
	GetNodeMetric(metricName string) (NodeMetricsInfo, error)
}

// NodeMetric holds information on a single piece of telemetry data.
type NodeMetric struct {
	Timestamp time.Time
	Window    time.Duration
	Value     resource.Quantity
}

// NodeMetricsInfo holds a map of metric information related to a single named metric. The key for the map is the name of the node.
type NodeMetricsInfo map[string]NodeMetric

// CustomMetricsClient embeds a client for the custom Metrics API.
type CustomMetricsClient struct {
	customclient.CustomMetricsClient
}

// NewClient creates a new Metrics Client including discovering and mapping the available APIs, and pulling the API version.
func NewClient(config *restclient.Config) CustomMetricsClient {
	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(config)
	cachedDiscoveryClient := cacheddiscovery.NewMemCacheClient(discoveryClient)
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscoveryClient)
	restMapper.Reset()

	apiVersionsGetter := customclient.NewAvailableAPIsGetter(discoveryClient)
	metricsClient := CustomMetricsClient{customclient.NewForConfig(config, restMapper, apiVersionsGetter)}

	return metricsClient
}

// GetNodeMetric gets the given metric, time Window for Metric and timestamp for each node in the cluster.
func (c CustomMetricsClient) GetNodeMetric(metricName string) (NodeMetricsInfo, error) {
	metrics, err := c.RootScopedMetrics().GetForObjects(schema.GroupKind{Kind: "Node"}, labels.NewSelector(), metricName, labels.NewSelector())
	if err != nil {
		return nil, fmt.Errorf("unable to get metric %v from custom metrics API: %w", metricName, err)
	}

	if len(metrics.Items) == 0 {
		return nil, fmt.Errorf("metric %v not in custom metrics API %w", metricName, errNull)
	}

	output := wrapMetrics(metrics)

	return output, nil
}

// wrapMetrics parses the custom metrics API MetricValueList type into a NodeCustomMetricInfo.
func wrapMetrics(metrics *v1beta2.MetricValueList) NodeMetricsInfo {
	result := make(NodeMetricsInfo, len(metrics.Items))

	for _, m := range metrics.Items {
		window := time.Minute
		if m.WindowSeconds != nil {
			window = time.Duration(*m.WindowSeconds) * time.Second
		}

		result[m.DescribedObject.Name] = NodeMetric{
			Timestamp: m.Timestamp.Time,
			Window:    window,
			Value:     m.Value,
		}
	}

	return result
}
