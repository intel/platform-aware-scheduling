// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Fuzz tests for the scheduler extender prioritize + filter methods
package telemetryscheduler

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	rnd "math/rand"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/intel/platform-aware-scheduling/extender"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	telpolv1 "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

type RuleOperator int64

const (
	Unknown RuleOperator = iota
	GreatherThan
	LessThan
	Equals
)

const (
	DontScheduleStrategyName      string = "dontschedule"
	ScheduleonmetricStrategyName  string = "scheduleonmetric"
	NodeNamePrefix                string = "nodeName"
	TasPolicyLabelName            string = "telemetry-policy"
	HealthMetricName              string = "health-metric"
	TasPolicyName                 string = "health-metric-policy" // tas policy label value
	HealthMetricDemoNamespaceName string = "health-metric-demo"
	HealthMetricDemoPodName       string = "health-metric-demo-pod"
	K8sResourceRegex              string = `^[a-z]{1,20}-[a-z]{1,20}-*[a-z]{0,20}-*[A-Za-z0-9_-]{0,20}$`
	DefaultMetricValue            int    = 27
)

var (
	operatorHandlers        = createRuleOperatorDontScheduleHandlers()
	k8sResourceRegexCompile = regexp.MustCompile(K8sResourceRegex)
)

func (r RuleOperator) GetRuleOperatorName() string {
	switch r {
	case GreatherThan:
		return "GreaterThan"
	case LessThan:
		return "LessThan"
	case Equals:
		return "Equals"
	}

	return "Unknown"
}

func createRuleOperatorDontScheduleHandlers() map[RuleOperator]func(resource.Quantity, int64) bool {
	return map[RuleOperator]func(resource.Quantity, int64) bool{
		LessThan: func(value resource.Quantity, target int64) bool {
			return value.CmpInt64(target) == -1
		},
		GreatherThan: func(value resource.Quantity, target int64) bool {
			return value.CmpInt64(target) == 1
		},
		Equals: func(value resource.Quantity, target int64) bool {
			return value.CmpInt64(target) == 0
		},
	}
}

func evaluateDontScheduleRule(value, target int64, operator RuleOperator) bool {
	if _, ok := operatorHandlers[operator]; !ok {
		klog.Warningf("Invalid operator type:" + operator.GetRuleOperatorName())

		return false
	}

	return operatorHandlers[operator](*resource.NewQuantity(value, resource.DecimalSI), target)
}

// The number of nodes available in a K8s cluster is a strict positive number.
func isNumberOfNodesInputValid(numberOfNodes int) bool {
	return numberOfNodes > 0
}

// Basic regex check for K8s resource name to force the fuzzer to use
// some valid values for the input parameters.
// Tt's not the purpose of this test to check that K8s resource names
// are valid.
func isK8sResourceNameInputValid(k8sResourceName string) bool {
	return k8sResourceRegexCompile.MatchString(k8sResourceName)
}

func areFilterFuzzTestInputParametersValid(numberOfNodes int, policyName, namespaceName, podName string) bool {
	return isNumberOfNodesInputValid(numberOfNodes) &&
		isK8sResourceNameInputValid(policyName) &&
		isK8sResourceNameInputValid(namespaceName) &&
		isK8sResourceNameInputValid(podName)
}

func getViolatingNodes(hasDontScheduleRule bool, nodeMetricValues []int, dontScheduleThreshold int, ruleOperator RuleOperator) int {
	if !hasDontScheduleRule {
		return 0
	}

	numberOfViolatingNodes := 0

	for _, item := range nodeMetricValues {
		if evaluateDontScheduleRule(int64(item), int64(dontScheduleThreshold), ruleOperator) {
			numberOfViolatingNodes++
		}
	}

	return numberOfViolatingNodes
}

func generateValidRandomMetricValue(value int) int {
	currentValue := value
	base := value

	if value == 0 {
		currentValue = DefaultMetricValue
		base = DefaultMetricValue
	} else if value < 0 {
		currentValue = -value
	}

	result, err := rand.Int(rand.Reader, big.NewInt(int64(currentValue)))
	if err != nil {
		klog.Warningf("Unable to generate a random int value for: %d. Will exit with current value", result)

		return value
	}

	return int(math.Pow(-1, float64(base))) * int(result.Int64())
}

func setUpMetricValues(numberOfNodes int, dontScheduleThreshold int) []int {
	values := make([]int, numberOfNodes)
	maxMetricValue := dontScheduleThreshold + generateValidRandomMetricValue(dontScheduleThreshold)

	for i := 0; i < numberOfNodes; i++ {
		values[i] = generateValidRandomMetricValue(maxMetricValue)
	}

	rnd.Shuffle(len(values), func(i, j int) {
		values[i], values[j] = values[j], values[i]
	})

	return values
}

func setUpNodeCache(t *testing.T, metricName string, numberOfNodes int, values []int) *cache.AutoUpdatingCache {
	selfUpdatingCache := cache.MockEmptySelfUpdatingCache()

	if numberOfNodes != len(values) {
		return selfUpdatingCache.(*cache.AutoUpdatingCache)
	}

	nodeNames := []string{}
	nodeValues := []int64{}

	for i := 0; i < numberOfNodes; i++ {
		genericNodeName := fmt.Sprintf("%s%d", NodeNamePrefix, i+1)
		nodeNames = append(nodeNames, genericNodeName)
		nodeValues = append(nodeValues, int64(values[i]))
	}

	err := selfUpdatingCache.WriteMetric(metricName, metrics.TestNodeMetricCustomInfo(nodeNames, nodeValues))
	if err != nil {
		t.Errorf("Unable to write metric %s to cache. Error : %v", metricName, err)
	}

	return selfUpdatingCache.(*cache.AutoUpdatingCache)
}

func setupDontSchedulePolicy(policyName, policyNamespace, metricName string, hasDontScheduleRule bool,
	dontScheduleThreshold int, ruleOperator RuleOperator) telpolv1.TASPolicy {
	var policySpec = map[string]telpolv1.TASPolicyStrategy{
		ScheduleonmetricStrategyName: {
			PolicyName: policyName,
			Rules: []telpolv1.TASPolicyRule{
				{Metricname: metricName, Operator: ruleOperator.GetRuleOperatorName(), Target: 0}},
		},
	}

	if hasDontScheduleRule {
		policySpec[DontScheduleStrategyName] = telpolv1.TASPolicyStrategy{
			PolicyName: policyName,
			Rules: []telpolv1.TASPolicyRule{
				{Metricname: metricName, Operator: ruleOperator.GetRuleOperatorName(), Target: int64(dontScheduleThreshold)}},
		}
	}

	return telpolv1.TASPolicy{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: policyNamespace},
		Spec: telpolv1.TASPolicySpec{
			Strategies: policySpec},
		Status: telpolv1.TASPolicyStatus{},
	}
}

func setupPodSpec(podName, podNamespace, labelMapKey, labelMapValue string) v1.Pod {
	return v1.Pod{TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: podName, Labels: map[string]string{labelMapKey: labelMapValue}, Namespace: podNamespace}}
}

func setupExtenderArgs(podName, podNamespace, labelMapKey, labelMapValue string, numberOfNodes int) extender.Args {
	nodes := make([]v1.Node, numberOfNodes)
	nodeNames := make([]string, numberOfNodes)

	for i := 0; i < numberOfNodes; i++ {
		genericNodeName := fmt.Sprintf("%s%d", NodeNamePrefix, i+1)
		nodes[i] = v1.Node{TypeMeta: metav1.TypeMeta{}, ObjectMeta: metav1.ObjectMeta{Name: genericNodeName}, Spec: v1.NodeSpec{}, Status: v1.NodeStatus{}}
		nodeNames[i] = genericNodeName
	}

	return extender.Args{
		Pod:       setupPodSpec(podName, podNamespace, labelMapKey, labelMapValue),
		Nodes:     &v1.NodeList{Items: nodes},
		NodeNames: &nodeNames,
	}
}

func setupMetricExtender(t *testing.T, namespaceName string, selfUpdatingCache *cache.AutoUpdatingCache, policy telpolv1.TASPolicy) MetricsExtender {
	err := selfUpdatingCache.WritePolicy(namespaceName, policy.Name, policy)
	if err != nil {
		t.Errorf("Error while trying to add policy to self-updating cache: %v", err)
	}

	return MetricsExtender{
		cache: selfUpdatingCache,
	}
}

func convertExtenderArgsToJSON(t *testing.T, numberOfNodes int, podName, namespaceName, policyName string) []byte {
	argsAsJSON, err := json.Marshal(setupExtenderArgs(podName, namespaceName, TasPolicyLabelName, policyName, numberOfNodes))
	if err != nil {
		t.Errorf("Error trying to serialize extender.Args into JSON: %v ", err)
	}

	result := extender.FilterResult{}
	err = json.Unmarshal(argsAsJSON, &result)

	if err != nil {
		t.Errorf("Error trying to deserialize into FilterResult: %v", err)
	}

	return argsAsJSON
}

func validateFilterExpectations(t *testing.T, w *httptest.ResponseRecorder, hasDontScheduleRule bool, expectedNumberOfNodes,
	expectedNumberOfViolatingNodes int) {
	result := extender.FilterResult{}
	b := w.Body.Bytes()

	err := json.Unmarshal(b, &result)
	if err != nil {
		t.Errorf("Error trying to serialize FilterResult into JSON %v", err)
	}

	gotNumberOfNodes := len(result.Nodes.Items)
	gotNumberOfViolatingNodes := len(result.FailedNodes)

	if hasDontScheduleRule {
		if gotNumberOfNodes != (expectedNumberOfNodes - expectedNumberOfViolatingNodes) {
			t.Errorf("Expected different number of non-violating nodes. Expected %d, got %d", (expectedNumberOfNodes - expectedNumberOfViolatingNodes), gotNumberOfNodes)
		}

		if gotNumberOfViolatingNodes != expectedNumberOfViolatingNodes {
			t.Errorf("Expected different number of violating nodes. Expected %d, got %d", expectedNumberOfViolatingNodes, gotNumberOfViolatingNodes)
		}
	} else {
		if gotNumberOfViolatingNodes != 0 {
			t.Errorf("Expected 0 violating nodes, got %d", gotNumberOfViolatingNodes)
		}

		if gotNumberOfNodes != expectedNumberOfNodes {
			t.Errorf("Unexpected number of non-violating nodes. Expected %d, got %d", expectedNumberOfNodes, gotNumberOfNodes)
		}
	}
}

func FuzzMetricsExtenderFilter(f *testing.F) {
	f.Add(true, 0, 3, 1, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, -20, 3, 1, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(true, 60, 3, 1, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, 37, 5, 1, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(true, 25, 2, 2, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, 57, 5, 2, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(true, 90, 40, 3, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, 90, 5, 3, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(true, 43, 9, -39, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, 43, 1, -39, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)

	f.Fuzz(func(t *testing.T, hasDontScheduleRule bool, dontScheduleThreshold, numberOfNodes, ruleOperatorType int,
		metricName, policyName, namespaceName, podName string) {
		ruleOperator := RuleOperator(ruleOperatorType)
		if !areFilterFuzzTestInputParametersValid(numberOfNodes, policyName, namespaceName, podName) {
			return
		}

		metricValues := setUpMetricValues(numberOfNodes, dontScheduleThreshold)
		numberOfViolatingNodes := getViolatingNodes(hasDontScheduleRule, metricValues, dontScheduleThreshold, ruleOperator)
		policy := setupDontSchedulePolicy(policyName, namespaceName, metricName, hasDontScheduleRule, dontScheduleThreshold, ruleOperator)
		selfUpdatingCache := setUpNodeCache(t, metricName, numberOfNodes, metricValues)
		m := setupMetricExtender(t, namespaceName, selfUpdatingCache, policy)
		extenderArgs := convertExtenderArgsToJSON(t, numberOfNodes, podName, namespaceName, policyName)

		mockedRequest := &http.Request{}
		mockedRequest.Body = io.NopCloser(bytes.NewReader(extenderArgs))
		mockedRequest.Header = http.Header{}
		mockedRequest.Header.Add("Content-Type", "application/json")

		w := httptest.NewRecorder()
		m.Filter(w, mockedRequest)
		validateFilterExpectations(t, w, hasDontScheduleRule, numberOfNodes, numberOfViolatingNodes)
	})
}
