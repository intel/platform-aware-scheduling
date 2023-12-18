// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package telemetryscheduler logic contains specific code for TAS such as prioritize and filter methods.
package telemetryscheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	telemetrypolicy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	extenderV1 "k8s.io/kube-scheduler/extender/v1"
)

const (
	l2       = 2
	l4       = 4
	maxScore = 10
)

var (
	tasPolicy       = "telemetry-policy"
	errReqBody      = errors.New("request body empty")
	errNonode       = errors.New("no nodes in the list")
	errNoPolicy     = errors.New("no policy found")
	errNoRules      = errors.New("no rules found")
	errDontschedule = errors.New("dontschedule not found")
	errNull         = errors.New("")
)

// MetricsExtender holds information on the cache holding scheduling strategies and metrics.
type MetricsExtender struct {
	cache cache.Reader
}

// NewMetricsExtender returns a new metric Extender with the cache passed to it.
func NewMetricsExtender(newCache cache.Reader) MetricsExtender {
	return MetricsExtender{
		cache: newCache,
	}
}

// Prioritize manages all prioritize requests from the scheduler extender.
// It decodes the package, checks its policy, and performs error checking.
// It then calls the prioritize logic and writes a response to the scheduler.
func (m MetricsExtender) Prioritize(w http.ResponseWriter, r *http.Request) {
	klog.V(l2).InfoS("Received prioritize request", "component", "extender")

	extenderArgs, err := m.DecodeExtenderRequest(r)
	if err != nil {
		klog.V(l2).InfoS("failed to prioritize: "+err.Error(), "component", "extender")

		return
	}

	if len(extenderArgs.Nodes.Items) == 0 {
		klog.V(l2).InfoS("bad extender arguments. No nodes in list", "component", "extender")

		return
	}

	if _, ok := extenderArgs.Pod.Labels[tasPolicy]; !ok {
		klog.V(l2).InfoS("no policy associated with pod", "component", "extender")
		w.WriteHeader(http.StatusBadRequest)
	}

	prioritizedNodes := m.prioritizeNodes(extenderArgs)

	if prioritizedNodes == nil {
		w.WriteHeader(http.StatusNotFound)
	}

	m.WritePrioritizeResponse(w, prioritizedNodes)
}

// DecodeExtenderRequest reads the json request into the expected struct.
// It returns an error of the request is not in the required format.
func (m MetricsExtender) DecodeExtenderRequest(r *http.Request) (extenderV1.ExtenderArgs, error) {
	var args extenderV1.ExtenderArgs
	if r.Body == nil {
		return args, fmt.Errorf("%w", errReqBody)
	}

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		return args, fmt.Errorf("error decoding request: %w", err)
	}

	if err := r.Body.Close(); err != nil {
		return args, fmt.Errorf("cannot decode request %w", err)
	}

	if args.Nodes == nil {
		return args, fmt.Errorf("%w", errNonode)
	}

	return args, nil
}

// prioritizeNodes implements the logic for the prioritize scheduler call.
func (m MetricsExtender) prioritizeNodes(args extenderV1.ExtenderArgs) *extenderV1.HostPriorityList {
	policy, err := m.getPolicyFromPod(args.Pod)
	if err != nil {
		klog.V(l2).InfoS("get policy from pod failed: "+err.Error(), "component", "extender")

		return &extenderV1.HostPriorityList{}
	}

	scheduleRule, err := m.getSchedulingRule(policy)
	if err != nil {
		klog.V(l2).InfoS("get scheduling rule from policy failed: "+err.Error(), "component", "extender")

		return &extenderV1.HostPriorityList{}
	}

	chosenNodes, err := m.prioritizeNodesForRule(scheduleRule, args.Nodes)
	if err != nil {
		klog.V(l2).InfoS(err.Error(), "component", "extender")

		return &extenderV1.HostPriorityList{}
	}

	msg := fmt.Sprintf("node priorities returned: %v", chosenNodes)
	klog.V(l2).InfoS(msg, "component", "extender")

	return &chosenNodes
}

// getPolicyFromPod returns the policy associated with a pod, if declared, from the api.
func (m MetricsExtender) getPolicyFromPod(pod *v1.Pod) (telemetrypolicy.TASPolicy, error) {
	if policyName, ok := pod.Labels["telemetry-policy"]; ok {
		policy, err := m.cache.ReadPolicy(pod.Namespace, policyName)
		if err != nil {
			return telemetrypolicy.TASPolicy{}, fmt.Errorf("failed to read policy: %w", err)
		}

		return policy, nil
	}

	return telemetrypolicy.TASPolicy{}, fmt.Errorf("pod spec for pod %v: %w", pod.Name, errNoPolicy)
}

// getSchedulingRule does basic validation on the scheduling rule. Returns the rule if it seems useful.
func (m MetricsExtender) getSchedulingRule(policy telemetrypolicy.TASPolicy) (telemetrypolicy.TASPolicyRule, error) {
	_, ok := policy.Spec.Strategies[scheduleonmetric.StrategyType]
	if ok && len(policy.Spec.Strategies[scheduleonmetric.StrategyType].Rules) > 0 {
		out := policy.Spec.Strategies[scheduleonmetric.StrategyType].Rules[0]
		if len(out.Metricname) > 0 {
			return out, nil
		}
	}

	return telemetrypolicy.TASPolicyRule{}, fmt.Errorf("failed to schedule: %w", errNoRules)
}

// prioritizeNodesForRule returns the nodes listed in order of priority after applying the appropriate telemetry rule.
// Priorities are ordinal - there is no relationship between the outputted priorities and the metrics - simply an order of preference.
func (m MetricsExtender) prioritizeNodesForRule(rule telemetrypolicy.TASPolicyRule, nodes *v1.NodeList) (extenderV1.HostPriorityList, error) {
	filteredNodeData := metrics.NodeMetricsInfo{}

	nodeData, err := m.cache.ReadMetric(rule.Metricname)
	if err != nil {
		return nil, fmt.Errorf("failed to prioritize: %w, %v ", err, rule.Metricname)
	}
	// Here we pull out nodes that have metrics but aren't in the filtered list
	for _, node := range nodes.Items {
		if v, ok := nodeData[node.Name]; ok {
			filteredNodeData[node.Name] = v
		}
	}

	outputNodes := extenderV1.HostPriorityList{}

	metricsOutput := fmt.Sprintf("%v for nodes: ", rule.Metricname)
	orderedNodes := core.OrderedList(filteredNodeData, rule.Operator)

	for i, node := range orderedNodes {
		metricsOutput = fmt.Sprint(metricsOutput, " [ ", node.NodeName, " :", node.MetricValue.AsDec(), "]")

		outputNodes = append(outputNodes, extenderV1.HostPriority{Host: node.NodeName, Score: int64(maxScore - i)})
	}

	klog.V(l2).InfoS(metricsOutput, "component", "extender")

	return outputNodes, nil
}

// WritePrioritizeResponse writes out the results of prioritize in the response to the scheduler.
func (m MetricsExtender) WritePrioritizeResponse(w http.ResponseWriter, result *extenderV1.HostPriorityList) {
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(result); err != nil {
		klog.V(l4).InfoS("Encode error: "+err.Error(), "component", "extender")
		http.Error(w, "Encode error ", http.StatusBadRequest)
	}
}

// Filter manages all filter requests from the scheduler.
// It decodes the request, checks its policy and registers it.
// It then calls the filter logic and writes a response to the scheduler.
func (m MetricsExtender) Filter(w http.ResponseWriter, r *http.Request) {
	klog.V(l2).InfoS("Filter request received", "component", "extender")

	extenderArgs, err := m.DecodeExtenderRequest(r)
	if err != nil {
		klog.V(l2).InfoS("cannot filter "+err.Error(), "component", "extender")

		return
	}

	filteredNodes := m.filterNodes(extenderArgs)

	if filteredNodes == nil {
		klog.V(l2).InfoS("No filtered nodes returned", "component", "extender")
		w.WriteHeader(http.StatusNotFound)
	}

	m.WriteFilterResponse(w, filteredNodes)
}

// Bind binds the pod to the node. Not implemented by TAS, hence response with StatusNotFound.
func (m MetricsExtender) Bind(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotFound)
}

// filterNodes takes in the arguments for the scheduler and filters nodes based on the pod's dontschedule strategy - if it has one in an attached policy.
func (m MetricsExtender) filterNodes(args extenderV1.ExtenderArgs) *extenderV1.ExtenderFilterResult {
	availableNodeNames := ""

	var filteredNodes []v1.Node

	failedNodes := extenderV1.FailedNodesMap{}
	result := extenderV1.ExtenderFilterResult{}

	policy, err := m.getPolicyFromPod(args.Pod)
	if err != nil {
		klog.V(l2).InfoS("get policy from pod failed "+err.Error(), "component", "extender")

		return nil
	}

	dontscheduleStrategy, err := m.getDontScheduleStrategy(policy)
	if err != nil {
		klog.V(l4).InfoS("Returning all nodes "+err.Error(), "component", "extender")

		return &extenderV1.ExtenderFilterResult{
			Nodes: args.Nodes,
		}
	}

	violatingNodes := dontscheduleStrategy.Violated(m.cache)

	if len(args.Nodes.Items) == 0 {
		klog.V(l2).InfoS("No nodes to compare", "component", "extender")

		return nil
	}

	for _, node := range args.Nodes.Items {
		if _, ok := violatingNodes[node.Name]; ok {
			failedNodes[node.Name] = strings.Join([]string{"Node violates"}, policy.Name)
		} else {
			filteredNodes = append(filteredNodes, node)
			availableNodeNames += node.Name + " "
		}
	}

	nodeNames := strings.Split(availableNodeNames, " ")
	result = extenderV1.ExtenderFilterResult{
		Nodes: &v1.NodeList{
			Items: filteredNodes,
		},
		NodeNames:   &nodeNames,
		FailedNodes: failedNodes,
		Error:       "",
	}

	if len(availableNodeNames) > 0 {
		klog.V(l2).InfoS("Filtered nodes for "+policy.Name+": "+availableNodeNames, "component", "extender")
	}

	return &result
}

// getDontScheduleStrategy pulls the dontschedule strategy from a telemetry policy passed to it.
func (m MetricsExtender) getDontScheduleStrategy(policy telemetrypolicy.TASPolicy) (dontschedule.Strategy, error) {
	rawStrategy := policy.Spec.Strategies[dontschedule.StrategyType]

	if len(rawStrategy.Rules) == 0 {
		return dontschedule.Strategy{}, fmt.Errorf("strategy failed: %w", errDontschedule)
	}

	dontscheduleStrategy := (dontschedule.Strategy)(rawStrategy)

	return dontscheduleStrategy, nil
}

// WriteFilterResponse takes the ExtenderFilterResults struct and writes it as a http response if valid.
func (m MetricsExtender) WriteFilterResponse(w http.ResponseWriter, result *extenderV1.ExtenderFilterResult) {
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(result); err != nil {
		klog.V(l4).InfoS("Encode error "+err.Error(), "component", "extender")
		http.Error(w, "Encode error", http.StatusBadRequest)
	}
}
