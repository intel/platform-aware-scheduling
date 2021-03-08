//Package telemetryscheduler logic contains specific code for TAS such as prioritize and filter methods
package telemetryscheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/intel/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/telemetry-aware-scheduling/pkg/scheduler"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/core"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	telemetrypolicy "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"log"
	"net/http"
	"strings"
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
	log.Print("Received prioritize request")
	extenderArgs, err := m.DecodeExtenderRequest(r)
	if err != nil {
		log.Printf("failed to prioritize %v", err)
		return
	}
	if len(extenderArgs.Nodes.Items) == 0 {
		log.Print("bad extender arguments. No nodes in list")
		return
	}
	if _, ok := extenderArgs.Pod.Labels[tasPolicy]; !ok {
		log.Printf("no policy associated with pod")
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
func (m MetricsExtender) DecodeExtenderRequest(r *http.Request) (scheduler.ExtenderArgs, error) {
	var args scheduler.ExtenderArgs
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
func (m MetricsExtender) prioritizeNodes(args scheduler.ExtenderArgs) *scheduler.HostPriorityList {
	policy, err := m.getPolicyFromPod(&args.Pod)
	if err != nil {
		log.Print(err)
		return &scheduler.HostPriorityList{}
	}
	scheduleRule, err := m.getSchedulingRule(policy)
	if err != nil {
		log.Print(err)
		return &scheduler.HostPriorityList{}
	}
	chosenNodes, err := m.prioritizeNodesForRule(scheduleRule, args.Nodes)
	if err != nil {
		log.Print(err)
		return &scheduler.HostPriorityList{}
	}
	log.Printf("node priorities returned: %v", chosenNodes)
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
func (m MetricsExtender) prioritizeNodesForRule(rule telemetrypolicy.TASPolicyRule, nodes *v1.NodeList) (scheduler.HostPriorityList, error) {
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
	outputNodes := scheduler.HostPriorityList{}
	metricsOutput := fmt.Sprintf("%v for nodes: ", rule.Metricname)
	orderedNodes := core.OrderedList(filteredNodeData, rule.Operator)
	for i, node := range orderedNodes {
		metricsOutput = fmt.Sprint(metricsOutput, " [ ", node.NodeName, " :", node.MetricValue.AsDec(), "]")
		outputNodes = append(outputNodes, scheduler.HostPriority{Host: node.NodeName, Score: 10 - i})
	}
	log.Print(metricsOutput)
	return outputNodes, nil
}

//WritePrioritizeResponse writes out the results of prioritize in the response to the scheduler.
func (m MetricsExtender) WritePrioritizeResponse(w http.ResponseWriter, result *scheduler.HostPriorityList) {
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(result); err != nil {
		http.Error(w, "Encode error ", http.StatusBadRequest)
	}
}

//Filter manages all filter requests from the scheduler.
//It decodes the request, checks its policy and registers it.
//It then calls the filter logic and writes a response to the scheduler.
func (m MetricsExtender) Filter(w http.ResponseWriter, r *http.Request) {
	log.Print("filter request received")
	extenderArgs, err := m.DecodeExtenderRequest(r)
	if err != nil {
		log.Printf("cannot filter %v", err)
		return
	}
	filteredNodes := m.filterNodes(extenderArgs)
	if filteredNodes == nil {
		log.Print("No filtered nodes returned")
		w.WriteHeader(http.StatusNotFound)
	}
	m.WriteFilterResponse(w, filteredNodes)
}

//filterNodes takes in the arguments for the scheduler and filters nodes based on the pod's dontschedule strategy - if it has one in an attached policy.
func (m MetricsExtender) filterNodes(args scheduler.ExtenderArgs) *scheduler.ExtenderFilterResult {
	availableNodeNames := ""
	var filteredNodes []v1.Node
	failedNodes := scheduler.FailedNodesMap{}
	result := scheduler.ExtenderFilterResult{}
	policy, err := m.getPolicyFromPod(&args.Pod)
	if err != nil {
		log.Print(err)
		return nil
	}
	dontscheduleStrategy, err := m.getDontScheduleStrategy(policy)
	if err != nil {
		log.Print(err)
		return nil
	}
	violatingNodes := dontscheduleStrategy.Violated(m.cache)
	if len(args.Nodes.Items) == 0 {
		log.Print("No nodes to compare ")
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
	result = scheduler.ExtenderFilterResult{
		Nodes: &v1.NodeList{
			Items: filteredNodes,
		},
		NodeNames:   &nodeNames,
		FailedNodes: failedNodes,
		Error:       "",
	}
	if len(availableNodeNames) > 0 {
		log.Printf("Filtered nodes for %v : %v", policy.Name, availableNodeNames)
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
func (m MetricsExtender) WriteFilterResponse(w http.ResponseWriter, result *scheduler.ExtenderFilterResult) {
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(result); err != nil {
		http.Error(w, "Encode error", http.StatusBadRequest)
	}
}