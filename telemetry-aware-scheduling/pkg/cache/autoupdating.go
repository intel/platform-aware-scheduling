// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	telemetrypolicy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/klog/v2"
)

const (
	policyPath string = "policies/%v/%v"
	metricPath string = "metrics/%v"
	l2                = 2
)

var (
	errNull              = errors.New("")
	errInvalidMetricName = errors.New("invalid metric name")
	errInvalidPolicyName = errors.New("invalid policy name")
)

// AutoUpdatingCache holds a map of metrics of interest with their associated NodeMetricsInfo object.
type AutoUpdatingCache struct {
	concurrentCache
	mtx       sync.RWMutex
	metricMap map[string]int
}

// NewAutoUpdatingCache returns an empty metrics cache.
func NewAutoUpdatingCache() *AutoUpdatingCache {
	return &AutoUpdatingCache{
		concurrentCache: concurrentCache{
			cache: make(chan request),
		},
		metricMap: make(map[string]int),
	}
}

// PeriodicUpdate updates all the metrics in the Cache periodically based on a ticker passed to it.
func (n *AutoUpdatingCache) PeriodicUpdate(period time.Ticker, client metrics.Client, initialData map[string]interface{}) {
	go n.run(n.cache, initialData)

	for {
		n.updateAllMetrics(client)
		<-period.C
	}
}

// updateAllMetrics performs an updateAllMetrics to every metric in the cache.
func (n *AutoUpdatingCache) updateAllMetrics(client metrics.Client) {
	n.mtx.Lock()
	defer n.mtx.Unlock()

	for name := range n.metricMap {
		if len(name) > 0 {
			err := n.updateMetric(client, name)
			if err != nil {
				klog.V(l2).ErrorS(err, "failed to update metrics", "component", "controller")
			}
		} else {
			delete(n.metricMap, name)
		}
	}
}

// updateMetric updates the NodeMetricInfo object in the AutoUpdatingCache for a metric with a given name.
func (n *AutoUpdatingCache) updateMetric(client metrics.Client, metricName string) error {
	metricInfo, err := client.GetNodeMetric(metricName)
	if err != nil {
		return fmt.Errorf("get nodes metric: %w", err)
	}

	err = n.WriteMetric(metricName, metricInfo)
	if err != nil {
		return fmt.Errorf("%w : %v", err, metricName)
	}

	return nil
}

// ReadMetric returns the NodeMetricsInfo object for the passed named metric.
// If no metric of that name is found it returns an error.
func (n *AutoUpdatingCache) ReadMetric(metricName string) (metrics.NodeMetricsInfo, error) {
	key := fmt.Sprintf(metricPath, metricName)
	value := n.read(key)

	if metric, ok := value.(metrics.NodeMetricsInfo); ok {
		if metric != nil {
			return metric, nil
		}
	}

	return metrics.NodeMetricsInfo{}, fmt.Errorf("no metric %v found %w", metricName, errNull)
}

// ReadPolicy returns the policy object under the passed name and namespace from the cache.
func (n *AutoUpdatingCache) ReadPolicy(namespace string, policyName string) (telemetrypolicy.TASPolicy, error) {
	key := fmt.Sprintf(policyPath, namespace, policyName)
	value := n.read(key)

	if policy, ok := value.(telemetrypolicy.TASPolicy); ok {
		return policy, nil
	}

	return telemetrypolicy.TASPolicy{}, fmt.Errorf("no policy %v found %w", policyName, errNull)
}

// WritePolicy sends the passed object to be stored in the cache under the namespace/name.
func (n *AutoUpdatingCache) WritePolicy(namespace string, policyName string, policy telemetrypolicy.TASPolicy) error {
	if len(policyName) == 0 {
		klog.V(l2).ErrorS(errInvalidPolicyName, "Failed to add policy with name: "+policyName, "component", "controller")

		return errInvalidPolicyName
	}

	n.add(fmt.Sprintf(policyPath, namespace, policyName), policy)

	return nil
}

// WriteMetric first checks if there's any data with the request and then sends the request to the cache.
// It also increments a counter showing how many strategies are using the metric -
// protecting it from deletion until there are no more associated strategies.
func (n *AutoUpdatingCache) WriteMetric(metricName string, data metrics.NodeMetricsInfo) error {
	if len(metricName) == 0 {
		klog.V(l2).ErrorS(errInvalidMetricName, "Failed to write metric with metric name: "+metricName, "component", "controller")

		return errInvalidMetricName
	}

	payload := nilPayloadCheck(data)
	n.add(fmt.Sprintf(metricPath, metricName), payload)

	if payload == nil {
		n.mtx.Lock()
		defer n.mtx.Unlock()

		if total, ok := n.metricMap[metricName]; ok {
			n.metricMap[metricName] = total + 1
		} else {
			n.metricMap[metricName] = 1
		}
	}

	return nil
}

// DeletePolicy removes the policy removes the policy object at the given namespace/name string from the cache.
func (n *AutoUpdatingCache) DeletePolicy(namespace string, policyName string) error {
	klog.V(l2).InfoS("deleting "+fmt.Sprintf(policyPath, namespace, policyName), "component", "controller")
	n.delete(fmt.Sprintf(policyPath, namespace, policyName))

	return nil
}

// DeleteMetric keeps track of the number of policies currently using this metric. It is removed from the cache,
// if there are no policies associated with this metric.
func (n *AutoUpdatingCache) DeleteMetric(metricName string) error {
	n.mtx.Lock()
	if total, ok := n.metricMap[metricName]; ok && total == 1 {
		delete(n.metricMap, metricName)
		n.delete(fmt.Sprintf(metricPath, metricName))
	} else {
		n.metricMap[metricName] = total - 1
	}
	n.mtx.Unlock()

	return nil
}

// nilPayloadCheck replaces the payload with a nil value if there's no metrics attached.
// This prevents metrics from being overwritten with empty data on new additions.
func nilPayloadCheck(data metrics.NodeMetricsInfo) interface{} {
	var payload interface{}
	payload = nil

	if len(data) > 0 {
		payload = data
	}

	return payload
}
