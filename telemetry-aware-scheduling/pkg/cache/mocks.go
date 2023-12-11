// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package cache

// This file contains mock methods and objects which are used to test across the TAS packages.
import (
	"fmt"
	"time"

	"k8s.io/klog/v2"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	telemetrypolicy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MockCache is used in the tests for the core and other packages.
type MockCache struct {
	MockCache interface{}
}

const (
	timeSec                         = 100
	timeNs                          = 1
	unableToCreateDummyMetricString = "Unable to create a dummymetric"
)

// MockEmptySelfUpdatingCache returns auto updating cache.
func MockEmptySelfUpdatingCache() ReaderWriter {
	n := NewAutoUpdatingCache()
	go n.PeriodicUpdate(*time.NewTicker(time.Second), metrics.NewDummyMetricsClient(map[string]metrics.NodeMetricsInfo{}), map[string]interface{}{})

	return n
}

// MockSelfUpdatingCache returns auto updating cache.
func MockSelfUpdatingCache() *AutoUpdatingCache {
	n := MockEmptySelfUpdatingCache()

	err := n.WriteMetric("dummyMetric1", TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{50, 30}))
	if err != nil {
		klog.InfoS(unableToCreateDummyMetricString+err.Error(), "component", "testing")
	}

	err = n.WriteMetric("dummyMetric2", TestNodeMetricCustomInfo([]string{"node 1", "node2"}, []int64{100, 200}))
	if err != nil {
		klog.InfoS(unableToCreateDummyMetricString+err.Error(), "component", "testing")
	}

	err = n.WriteMetric("dummyMetric3", TestNodeMetricCustomInfo([]string{"node Z", "node Y"}, []int64{8, 40000000}))
	if err != nil {
		klog.InfoS(unableToCreateDummyMetricString+err.Error(), "component", "testing")
	}

	return n.(*AutoUpdatingCache)
}

// TestNodeMetricCustomInfo returns the node metrics information.
func TestNodeMetricCustomInfo(nodeNames []string, numbers []int64) metrics.NodeMetricsInfo {
	n := metrics.NodeMetricsInfo{}
	for i, name := range nodeNames {
		n[name] = metrics.NodeMetric{Value: *resource.NewQuantity(numbers[i], resource.DecimalSI), Window: time.Second, Timestamp: time.Unix(timeSec, timeNs)}
	}

	return n
}

var mockPolicy = telemetrypolicy.TASPolicy{
	ObjectMeta: v1.ObjectMeta{Name: "mock-policy", Namespace: "default"},
}
var mockPolicy2 = telemetrypolicy.TASPolicy{
	ObjectMeta: v1.ObjectMeta{Name: "not-mock-policy", Namespace: "default"},
}
var mockInvalidPolicyName1 = telemetrypolicy.TASPolicy{
	ObjectMeta: v1.ObjectMeta{Name: "", Namespace: "default"},
}
var mockInvalidPolicyName2 = telemetrypolicy.TASPolicy{
	ObjectMeta: v1.ObjectMeta{Name: "n", Namespace: "default"},
}

// ReadMetric is a method implemented for Mock cache.
func (n MockCache) ReadMetric(string) (metrics.NodeMetricsInfo, error) {
	return metrics.NodeMetricsInfo{}, nil
}

// ReadPolicy is a method implemented for Mock cache.
func (n MockCache) ReadPolicy(string, string) (telemetrypolicy.TASPolicy, error) {
	return telemetrypolicy.TASPolicy{}, nil
}

// WriteMetric is a method implemented for Mock cache.
func (n MockCache) WriteMetric(metricName string, _ metrics.NodeMetricsInfo) error {
	if metricName != "" {
		return nil
	}

	return fmt.Errorf("failed to write metric %w", errNull)
}

// WritePolicy is a method implemented for Mock cache.
func (n MockCache) WritePolicy(namespace string, _ string, _ telemetrypolicy.TASPolicy) error {
	if namespace != "default" {
		return fmt.Errorf("failed to write policy %w", errNull)
	}

	return nil
}

// DeleteMetric is a method implemented for Mock cache.
func (n MockCache) DeleteMetric(metricName string) error {
	if metricName != "" {
		return nil
	}

	return fmt.Errorf("no metric to delete %w", errNull)
}

// DeletePolicy is a method implemented for Mock cache.
func (n MockCache) DeletePolicy(namespace string, policyName string) error {
	if namespace != "default" || policyName == "" {
		return fmt.Errorf("failed to delete policy %w", errNull)
	}

	return nil
}
