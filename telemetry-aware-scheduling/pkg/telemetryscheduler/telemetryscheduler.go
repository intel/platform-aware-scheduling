//Package telemetryscheduler logic contains specific code for TAS such as prioritize and filter methods
package telemetryscheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/intel/telemetry-aware-scheduling/extender"
	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	telemetrypolicy "github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var tasPolicy = "telemetry-policy"

//MetricsExtender holds information on the cache holding scheduling strategies and metrics.
type MetricsExtender struct {
	cache cache.Reader
}

//NewMetricsExtender returns a new metric Extender with the cache passed to it.
func NewMetricsExtender(newCache cache.Reader) MetricsExtender {
	return MetricsExtender{
		cache: newCache,
	}
}

//Prioritize manages all prioritize requests from the scheduler extender.
//It decodes the package, checks its policy, and performs error checking.
//It then calls the prioritize logic and writes a response to the scheduler.
func (m MetricsExtender) Prioritize(w http.ResponseWriter, r *http.Request) {
	klog.V(2).InfoS("Received prioritize request", "component", "extender")
	extenderArgs, err := m.DecodeExtenderRequest(r)
	if err != nil {
		klog.V(2).InfoS("failed to prioritize: "+err.Error(), "component", "extender")
		return
	}
	if len(extenderArgs.Nodes.Items) == 0 {
		klog.V(2).InfoS("bad extender arguments. No nodes in list", "component", "extender")
		return
	}
	if _, ok := extenderArgs.Pod.Labels[tasPolicy]; !ok {
		klog.V(2).InfoS("no policy associated with pod", "component", "extender")
		w.WriteHeader(http.StatusBadRequest)
	}
	prioritizedNodes := m.prioritizeNodes(extenderArgs)
	if prioritizedNodes == nil {
		w.WriteHeader(http.StatusNotFound)
	}
	m.WritePrioritizeResponse(w, prioritizedNodes)
}

//DecodeExtenderRequest reads the json request into the expected struct.
//It returns an error of the request is not in the required format.
func (m MetricsExtender) DecodeExtenderRequest(r *http.Request) (extender.Args, error) {
	var args extender.Args
	if r.Body == nil {
		return args, fmt.Errorf("request body empty")
	}
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		return args, fmt.Errorf("error decoding request: %v", err)
	}
	if err := r.Body.Close(); err != nil {
		return args, fmt.Errorf("cannot decode request %v", err)
	}
	if args.Nodes == nil {
		return args, fmt.Errorf("no nodes in list")
	}
	return args, nil
}

//prioritizeNodes implements the logic for the prioritize scheduler call.
func (m MetricsExtender) prioritizeNodes(args extender.Args) *extender.HostPriorityList {
	policy, err := m.getPolicyFromPod(&args.Pod)
	if err != nil {
		klog.V(2).InfoS("get policy from pod failed: "+err.Error(), "component", "extender")
		return &extender.HostPriorityList{}
	}
	scheduleRule, err := m.getSchedulingRule(policy)
	if err != nil {
		klog.V(2).InfoS("get scheduling rule from policy failed: "+err.Error(), "component", "extender")
		return &extender.HostPriorityList{}
	}
	chosenNodes, err := m.prioritizeNodesForRule(scheduleRule, args.Nodes)
	if err != nil {
		klog.V(2).InfoS(err.Error(), "component", "extender")
		return &extender.HostPriorityList{}
	}
	msg := fmt.Sprintf("node priorities returned: %v", chosenNodes)
	klog.V(2).InfoS(msg, "component", "extender")
	return &chosenNodes
}

//getPolicyFromPod returns the policy associated with a pod, if declared, from the api.
func (m MetricsExtender) getPolicyFromPod(pod *v1.Pod) (telemetrypolicy.TASPolicy, error) {
	if policyName, ok := pod.Labels["telemetry-policy"]; ok {
		policy, err := m.cache.ReadPolicy(pod.Namespace, policyName)
		if err != nil {
			return telemetrypolicy.TASPolicy{}, err
		}
		return policy, nil
	}
	return telemetrypolicy.TASPolicy{}, fmt.Errorf("no policy found in pod spec for pod %v", pod.Name)
}

//Does basic validation on the scheduling rule. Returns the rule if it seems useful
func (m MetricsExtender) getSchedulingRule(policy telemetrypolicy.TASPolicy) (telemetrypolicy.TASPolicyRule, error) {
	_, ok := policy.Spec.Strategies[scheduleonmetric.StrategyType]
	if ok && len(policy.Spec.Strategies[scheduleonmetric.StrategyType].Rules) > 0 {
		out := policy.Spec.Strategies[scheduleonmetric.StrategyType].Rules[0]
		if len(out.Metricname) > 0 {
			return out, nil
		}
	}
	return telemetrypolicy.TASPolicyRule{}, errors.New("no scheduling rule found")
}

//prioritizeNodesForRule returns the nodes listed in order of priority after applying the appropriate telemetry rule.
//Priorities are ordinal - there is no relationship between the outputted priorities and the metrics - simply an order of preference.
func (m MetricsExtender) prioritizeNodesForRule(rule telemetrypolicy.TASPolicyRule, nodes *v1.NodeList) (extender.HostPriorityList, error) {
	filteredNodeData := metrics.NodeMetricsInfo{}
	nodeData, err := m.cache.ReadMetric(rule.Metricname)
	if err != nil {
		return nil, fmt.Errorf("failed to prioritize: %v, %v ", err, rule.Metricname)
	}
	// Here we pull out nodes that have metrics but aren't in the filtered list
	for _, node := range nodes.Items {
		if v, ok := nodeData[node.Name]; ok {
			filteredNodeData[node.Name] = v
		}
	}
	outputNodes := extender.HostPriorityList{}
	metricsOutput := fmt.Sprintf("%v for nodes: ", rule.Metricname)
	orderedNodes := core.OrderedList(filteredNodeData, rule.Operator)
	for i, node := range orderedNodes {
		metricsOutput = fmt.Sprint(metricsOutput, " [ ", node.NodeName, " :", node.MetricValue.AsDec(), "]")
		outputNodes = append(outputNodes, extender.HostPriority{Host: node.NodeName, Score: 10 - i})
	}
	klog.V(2).InfoS(metricsOutput, "component", "extender")
	return outputNodes, nil
}

//WritePrioritizeResponse writes out the results of prioritize in the response to the scheduler.
func (m MetricsExtender) WritePrioritizeResponse(w http.ResponseWriter, result *extender.HostPriorityList) {
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(result); err != nil {
		klog.V(4).InfoS("Encode error: "+err.Error(), "component", "extender")
		http.Error(w, "Encode error ", http.StatusBadRequest)
	}
}

//Filter manages all filter requests from the scheduler.
//It decodes the request, checks its policy and registers it.
//It then calls the filter logic and writes a response to the scheduler.
func (m MetricsExtender) Filter(w http.ResponseWriter, r *http.Request) {
	klog.V(2).InfoS("Filter request received", "component", "extender")
	extenderArgs, err := m.DecodeExtenderRequest(r)
	if err != nil {
		klog.V(2).InfoS("cannot filter "+err.Error(), "component", "extender")
		return
	}
	filteredNodes := m.filterNodes(extenderArgs)
	if filteredNodes == nil {
		klog.V(2).InfoS("No filtered nodes returned", "component", "extender")
		w.WriteHeader(http.StatusNotFound)
	}
	m.WriteFilterResponse(w, filteredNodes)
}

//filterNodes takes in the arguments for the scheduler and filters nodes based on the pod's dontschedule strategy - if it has one in an attached policy.
func (m MetricsExtender) filterNodes(args extender.Args) *extender.FilterResult {
	availableNodeNames := ""
	var filteredNodes []v1.Node
	failedNodes := extender.FailedNodesMap{}
	result := extender.FilterResult{}
	policy, err := m.getPolicyFromPod(&args.Pod)
	if err != nil {
		klog.V(2).InfoS("get policy from pod failed "+err.Error(), "component", "extender")
		return nil
	}
	dontscheduleStrategy, err := m.getDontScheduleStrategy(policy)
	if err != nil {
		klog.V(2).InfoS("Don't scheduler strategy failed "+err.Error(), "component", "extender")
		return nil
	}
	violatingNodes := dontscheduleStrategy.Violated(m.cache)
	if len(args.Nodes.Items) == 0 {
		klog.V(2).InfoS("No nodes to compare", "component", "extender")
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
	result = extender.FilterResult{
		Nodes: &v1.NodeList{
			Items: filteredNodes,
		},
		NodeNames:   &nodeNames,
		FailedNodes: failedNodes,
		Error:       "",
	}
	if len(availableNodeNames) > 0 {
		klog.V(2).InfoS("Filtered nodes for "+policy.Name+": "+availableNodeNames, "component", "extender")
	}
	return &result
}

//Pulls the dontschedule strategy from a telemetry policy passed to it
func (m MetricsExtender) getDontScheduleStrategy(policy telemetrypolicy.TASPolicy) (dontschedule.Strategy, error) {
	rawStrategy := policy.Spec.Strategies[dontschedule.StrategyType]
	if len(rawStrategy.Rules) == 0 {
		return dontschedule.Strategy{}, errors.New("no dontschedule strategy found")
	}
	dontscheduleStrategy := (dontschedule.Strategy)(rawStrategy)
	return dontscheduleStrategy, nil
}

//WriteFilterResponse takes the ExtenderFilterResults struct and writes it as a http response if valid.
func (m MetricsExtender) WriteFilterResponse(w http.ResponseWriter, result *extender.FilterResult) {
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(result); err != nil {
		klog.V(4).InfoS("Encode error "+err.Error(), "component", "extender")
		http.Error(w, "Encode error", http.StatusBadRequest)
	}
}
