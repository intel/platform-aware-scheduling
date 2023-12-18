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
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sort"
	"testing"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	telpolv1 "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	extenderV1 "k8s.io/kube-scheduler/extender/v1"
)

type RuleOperator int64

// NodeMetricMappingForSort type is necessary in order to call the sort.Slice method.
// Note lack of usage of time windows or stamps.
type NodeMetricMappingForSort struct {
	nodeName    string
	metricValue int
}

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
	PolicyNameRegex               string = `[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*`
	DefaultMetricValue            int    = 27
)

var (
	dontscheduleOperatorHandlers = createRuleOperatorDontScheduleHandlers()
	scheduleOnOperatorHandlers   = createRuleOperatorScheduleOnMetricHandlers()
	k8sResourceRegexCompile      = regexp.MustCompile(K8sResourceRegex)
	policyNameRegexCompile       = regexp.MustCompile(PolicyNameRegex)
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

func createRuleOperatorScheduleOnMetricHandlers() map[RuleOperator]func(nodeToMetricMapping []NodeMetricMappingForSort) []NodeMetricMappingForSort {
	return map[RuleOperator]func(nodeMetrics []NodeMetricMappingForSort) []NodeMetricMappingForSort{
		LessThan: func(nodeMetrics []NodeMetricMappingForSort) []NodeMetricMappingForSort {
			sort.Slice(nodeMetrics, func(i, j int) bool {
				return nodeMetrics[i].metricValue < nodeMetrics[j].metricValue
			})

			return nodeMetrics
		},
		GreatherThan: func(nodeMetrics []NodeMetricMappingForSort) []NodeMetricMappingForSort {
			sort.Slice(nodeMetrics, func(i, j int) bool {
				return nodeMetrics[i].metricValue > nodeMetrics[j].metricValue
			})

			return nodeMetrics
		},
	}
}

func evaluateDontScheduleRule(value, target int64, operator RuleOperator) bool {
	if _, ok := dontscheduleOperatorHandlers[operator]; !ok {
		klog.Warningf("Invalid operator type:" + operator.GetRuleOperatorName())

		return false
	}

	return dontscheduleOperatorHandlers[operator](*resource.NewQuantity(value, resource.DecimalSI), target)
}

func evaluateScheduleOnMetricRule(operator RuleOperator, filteredNodeData []NodeMetricMappingForSort) []NodeMetricMappingForSort {
	if _, ok := scheduleOnOperatorHandlers[operator]; !ok {
		klog.Warningf("Invalid operator type:" + operator.GetRuleOperatorName())

		return filteredNodeData
	}

	return scheduleOnOperatorHandlers[operator](filteredNodeData)
}

// The number of nodes available in a K8s cluster is a strict positive number.
func isNumberOfNodesInputValid(numberOfNodes int) bool {
	return numberOfNodes > 0
}

// When trying to deploy a TAS policy with an invalid string
// the CRD validation steps in an asks for a valid string and shows the user
// the format it requires.
func processPolicyNameInput(policyName string) string {
	if policyNameRegexCompile.MatchString(policyName) {
		// the CRD rejects even a partial match, adding this for feature parity
		policyNameMatch := string(policyNameRegexCompile.Find([]byte(policyName)))
		if policyNameMatch != policyName {
			klog.Warningf("Policy name %s did not fully match %s, will use a partial match as a policyName instead: %s",
				policyName, PolicyNameRegex, policyNameMatch)

			return ""
		}

		return policyName
	}

	return ""
}

func isPolicyNameValid(policyName string) bool {
	return len(policyName) > 0
}

// Basic regex check for K8s resource name to force the fuzzer to use
// some valid values for the input parameters.
// It's not the purpose of this test to check that K8s resource names
// are valid.
func isK8sResourceNameInputValid(k8sResourceName string) bool {
	return k8sResourceRegexCompile.MatchString(k8sResourceName)
}

func areFilterFuzzTestInputParametersValid(numberOfNodes int, policyName, namespaceName, podName string) bool {
	return isNumberOfNodesInputValid(numberOfNodes) &&
		isPolicyNameValid(policyName) &&
		isK8sResourceNameInputValid(namespaceName) &&
		isK8sResourceNameInputValid(podName)
}

func arePrioritizeFuzzTestInputParametersValid(numberOfNodes int, policyName, namespaceName, podName string) bool {
	return isNumberOfNodesInputValid(numberOfNodes) &&
		isPolicyNameValid(policyName) &&
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
func getMetricsPerNode(t *testing.T, selfUpdatingCache *cache.AutoUpdatingCache, metricName string) map[string]int {
	metricsInfo, err := selfUpdatingCache.ReadMetric(metricName)

	if err != nil {
		t.Errorf("Error when reading metric %s from self-updating cache %v", metricName, err)
	}

	nodeToMetricValueMapping := make(map[string]int)

	for nodeName, nodeInfo := range metricsInfo {
		value, _ := nodeInfo.Value.AsInt64()
		nodeToMetricValueMapping[nodeName] = int(value)
	}

	return nodeToMetricValueMapping
}

func getPrioritizedNodes(hasScheduleOnRule bool, ruleOperator RuleOperator,
	nodeMetrics map[string]int) extenderV1.HostPriorityList {
	if !hasScheduleOnRule {
		return extenderV1.HostPriorityList{}
	}

	filteredNodeData := []NodeMetricMappingForSort{}
	for nodeName, nodeValue := range nodeMetrics {
		filteredNodeData = append(filteredNodeData, NodeMetricMappingForSort{nodeName: nodeName, metricValue: nodeValue})
	}

	sortedNodeMetricValues := evaluateScheduleOnMetricRule(ruleOperator, filteredNodeData)
	prioritizedNodes := extenderV1.HostPriorityList{}

	for _, item := range sortedNodeMetricValues {
		prioritizedNodes = append(prioritizedNodes, extenderV1.HostPriority{Host: item.nodeName, Score: 0})
	}

	return prioritizedNodes
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

func setUpMetricValues(numberOfNodes int, metricThreshold int) []int {
	values := make([]int, numberOfNodes)
	maxMetricValue := metricThreshold + generateValidRandomMetricValue(metricThreshold)

	for i := 0; i < numberOfNodes; i++ {
		values[i] = generateValidRandomMetricValue(maxMetricValue)
	}

	// can't use math/rand.Shuffle as it's marked as a "weak method" by Checkmarx
	// instead, decided to implement something similar
	for i := 0; i < len(values); i++ {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(len(values))))
		if err != nil {
			klog.Warningf("Unable to generate a random int value for: %d. Will exit with current value", j)

			continue
		}

		correspondingIndex := int(j.Int64())
		values[i], values[correspondingIndex] = values[correspondingIndex], values[i]
	}

	return values
}

func getPolicyStrategy(policyName, metricName string, ruleOperator RuleOperator, threshold int) telpolv1.TASPolicyStrategy {
	return telpolv1.TASPolicyStrategy{
		PolicyName: policyName,
		Rules: []telpolv1.TASPolicyRule{
			{Metricname: metricName, Operator: ruleOperator.GetRuleOperatorName(), Target: int64(threshold)}},
	}
}

func setupDontSchedulePolicy(policyName, policyNamespace, metricName string, hasDontScheduleRule bool,
	dontScheduleThreshold int, ruleOperator RuleOperator) telpolv1.TASPolicy {
	var policySpec = map[string]telpolv1.TASPolicyStrategy{
		ScheduleonmetricStrategyName: getPolicyStrategy(policyName, metricName, ruleOperator, 0),
	}

	if hasDontScheduleRule {
		policySpec[DontScheduleStrategyName] = getPolicyStrategy(policyName, metricName, ruleOperator, dontScheduleThreshold)
	}

	return telpolv1.TASPolicy{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: policyNamespace},
		Spec: telpolv1.TASPolicySpec{
			Strategies: policySpec},
		Status: telpolv1.TASPolicyStatus{},
	}
}

func setupScheduleOnPolicy(policyName, policyNamespace, metricName string, hasScheduleOnRule bool, ruleOperator RuleOperator) telpolv1.TASPolicy {
	var policySpec = map[string]telpolv1.TASPolicyStrategy{}

	if hasScheduleOnRule {
		policySpec[ScheduleonmetricStrategyName] = getPolicyStrategy(policyName, metricName, ruleOperator, 0)
	}

	return telpolv1.TASPolicy{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: policyNamespace},
		Spec: telpolv1.TASPolicySpec{
			Strategies: policySpec},
		Status: telpolv1.TASPolicyStatus{},
	}
}

func setUpNodeCache(metricName string, numberOfNodes int, values []int) (*cache.AutoUpdatingCache, error) {
	selfUpdatingCache := cache.MockEmptySelfUpdatingCache()

	if numberOfNodes != len(values) {
		return selfUpdatingCache.(*cache.AutoUpdatingCache), nil
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
		return nil, fmt.Errorf("can't write metric to cache %s: %w", metricName, err)
	}

	return selfUpdatingCache.(*cache.AutoUpdatingCache), nil
}

func setupPodSpec(podName, podNamespace, labelMapKey, labelMapValue string) *v1.Pod {
	return &v1.Pod{TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: podName, Labels: map[string]string{labelMapKey: labelMapValue}, Namespace: podNamespace}}
}

func setupExtenderArgs(podName, podNamespace, labelMapKey, labelMapValue string, numberOfNodes int) extenderV1.ExtenderArgs {
	nodes := make([]v1.Node, numberOfNodes)
	nodeNames := make([]string, numberOfNodes)

	for i := 0; i < numberOfNodes; i++ {
		genericNodeName := fmt.Sprintf("%s%d", NodeNamePrefix, i+1)
		nodes[i] = v1.Node{TypeMeta: metav1.TypeMeta{}, ObjectMeta: metav1.ObjectMeta{Name: genericNodeName}, Spec: v1.NodeSpec{}, Status: v1.NodeStatus{}}
		nodeNames[i] = genericNodeName
	}

	return extenderV1.ExtenderArgs{
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

	result := extenderV1.ExtenderFilterResult{}
	err = json.Unmarshal(argsAsJSON, &result)

	if err != nil {
		t.Errorf("Error trying to deserialize into FilterResult: %v", err)
	}

	return argsAsJSON
}

func validateFilterExpectations(t *testing.T, w *httptest.ResponseRecorder, hasDontScheduleRule bool, expectedNumberOfNodes,
	expectedNumberOfViolatingNodes int) {
	result := extenderV1.ExtenderFilterResult{}
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

func validateMetricValues(nodeMetricValues map[string]int, expected, got extenderV1.HostPriorityList) bool {
	expectedValues := make([]int, 0)
	gotValues := make([]int, 0)

	for _, nodeName := range expected {
		expectedValues = append(expectedValues, nodeMetricValues[nodeName.Host])
	}

	for _, nodeName := range got {
		gotValues = append(gotValues, nodeMetricValues[nodeName.Host])
	}

	return reflect.DeepEqual(expectedValues, gotValues)
}

func validatePrioritizeExpectations(t *testing.T, hasDontScheduleRule bool, ruleOperator RuleOperator, numberOfNodes int,
	nodeMetricValues map[string]int, prioritizedNodes extenderV1.HostPriorityList, w *httptest.ResponseRecorder) {
	result := extenderV1.HostPriorityList{}
	b := w.Body.Bytes()

	err := json.Unmarshal(b, &result)
	if err != nil {
		t.Errorf("Error trying to serialize HostPriorityList into JSON %v", err)

		return
	}

	if hasDontScheduleRule {
		if len(result) != len(prioritizedNodes) {
			t.Errorf("Different number of nodes were returned via Prioritize: expected %d, got %d", len(prioritizedNodes), len(result))
		}

		if _, ok := scheduleOnOperatorHandlers[ruleOperator]; ok {
			if !validateMetricValues(nodeMetricValues, prioritizedNodes, result) {
				t.Errorf("Host names not equal. Expected %q and got %q", prioritizedNodes, result)
			}
		}
		// when Equals/Unknown is used we return an array with the nodes, in random order
		if numberOfNodes != len(result) {
			t.Errorf("Expected no nodes to be returned with %s operator: got %d", Equals.GetRuleOperatorName(), len(result))
		}
	} else if len(prioritizedNodes) != 0 {
		// when an Unknown rule operator type is used, we return an empty list of nodes
		t.Errorf("Expected 0 nodes to be returned with a missing scheduleonmetric rule: got %d", len(result))
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
		processedPolicyName := processPolicyNameInput(policyName)

		if !areFilterFuzzTestInputParametersValid(numberOfNodes, processedPolicyName, namespaceName, podName) {
			return
		}

		metricValues := setUpMetricValues(numberOfNodes, dontScheduleThreshold)
		numberOfViolatingNodes := getViolatingNodes(hasDontScheduleRule, metricValues, dontScheduleThreshold, ruleOperator)
		policy := setupDontSchedulePolicy(processedPolicyName, namespaceName, metricName, hasDontScheduleRule, dontScheduleThreshold, ruleOperator)
		selfUpdatingCache, err := setUpNodeCache(metricName, numberOfNodes, metricValues)
		if err != nil {
			// if we're here this most likely means we weren't able to add the metric values to the cache
			// chances are the metric name was invalid, There's no point in continuing the test with an invalid metric name
			return
		}
		m := setupMetricExtender(t, namespaceName, selfUpdatingCache, policy)
		extenderArgs := convertExtenderArgsToJSON(t, numberOfNodes, podName, namespaceName, processedPolicyName)

		mockedRequest := &http.Request{}
		mockedRequest.Body = io.NopCloser(bytes.NewReader(extenderArgs))
		mockedRequest.Header = http.Header{}
		mockedRequest.Header.Add("Content-Type", "application/json")

		w := httptest.NewRecorder()
		m.Filter(w, mockedRequest)
		validateFilterExpectations(t, w, hasDontScheduleRule, numberOfNodes, numberOfViolatingNodes)
	})
}

func FuzzMetricsExtenderPrioritize(f *testing.F) {
	f.Add(true, 3, 80, 1, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(true, 3, 1, 1, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, 5, 30, 1, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, 5, 3, 1, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, 5, 5, 1, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(true, 25, 2, 2, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, 57, 5, 2, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(true, 90, 40, 3, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, 90, 5, 3, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(true, 43, 9, -39, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)
	f.Add(false, 43, 1, -39, HealthMetricName, TasPolicyName, HealthMetricDemoNamespaceName, HealthMetricDemoPodName)

	f.Fuzz(func(t *testing.T, hasScheduleOnRule bool, numberOfNodes, maxMetricValue, ruleOperatorType int,
		metricName, policyName, namespaceName, podName string) {
		ruleOperator := RuleOperator(ruleOperatorType)
		processedPolicyName := processPolicyNameInput(policyName)

		if !arePrioritizeFuzzTestInputParametersValid(numberOfNodes, processedPolicyName, namespaceName, podName) {
			return
		}

		metricValues := setUpMetricValues(numberOfNodes, maxMetricValue)
		policy := setupScheduleOnPolicy(processedPolicyName, namespaceName, metricName, hasScheduleOnRule, ruleOperator)
		selfUpdatingCache, err := setUpNodeCache(metricName, numberOfNodes, metricValues)
		if err != nil {
			// if we're here this most likely means we weren't able to add the metric values to the cache
			// chances are the metric name was invalid, There's no point in continuing the test with an invalid metric name
			return
		}
		m := setupMetricExtender(t, namespaceName, selfUpdatingCache, policy)
		nodeMetricValues := getMetricsPerNode(t, selfUpdatingCache, metricName)
		prioritizedNodes := getPrioritizedNodes(hasScheduleOnRule, ruleOperator, nodeMetricValues)

		extenderArgs := convertExtenderArgsToJSON(t, numberOfNodes, podName, namespaceName, processedPolicyName)

		mockedRequest := &http.Request{}
		mockedRequest.Body = io.NopCloser(bytes.NewReader(extenderArgs))
		mockedRequest.Header = http.Header{}
		mockedRequest.Header.Add("Content-Type", "application/json")

		w := httptest.NewRecorder()
		m.Prioritize(w, mockedRequest)

		validatePrioritizeExpectations(t, hasScheduleOnRule, ruleOperator, numberOfNodes, nodeMetricValues, prioritizedNodes, w)
	})
}
