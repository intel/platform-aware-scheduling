// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package labeling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	strategy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

const (
	labelPrefix = "telemetry.aware.scheduling."
	pairValue   = 2
)

var errNull = errors.New("")

// node -> policy name -> labels slice (label is stored prefixed and the format is key=value).
type violationMap map[string]map[string][]string

// node -> all labels map (label is stored prefixed and the format is key=value).
type nodeViolations map[string]map[string]bool

type patchValue struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

func createLabelPatchValue(op, labelName, value string) *patchValue {
	return &patchValue{
		Op:    op,
		Path:  "/metadata/labels/" + labelName,
		Value: value,
	}
}

func getPrefix(policyName string) string {
	return labelPrefix + policyName + "/"
}

// Enforce describes the behavior followed by this strategy to return associated pods to non-violating status.
// The labels can be used externally for different purposes, e.g. by a descheduler.
// Here we make an api call to list all nodes first. This may be improved by using a controller instead or some
// other way of not waiting for the API call every time Enforce is called.
func (d *Strategy) Enforce(enforcer *strategy.MetricEnforcer, cache cache.Reader) (int, error) {
	nodes, err := enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		msg := fmt.Sprintf("cannot list nodes: %v", err)
		klog.V(l2).InfoS(msg, "component", "controller")

		return -1, fmt.Errorf("enforce failure (node list), %w", err)
	}

	violations, allNodeViolatedLabels := d.nodeStatusForStrategy(enforcer, cache)

	numberViolations, err := d.updateNodeLabels(enforcer, violations, allNodeViolatedLabels, nodes)
	if err != nil {
		klog.V(l2).InfoS(err.Error(), "component", "controller")

		return -1, err
	}

	return numberViolations, nil
}

// patchNode takes a json patch value and sends it to the API server to patch a node. Here it's used to label nodes.
func (d *Strategy) patchNode(nodeName string, enforcer *strategy.MetricEnforcer, payload []patchValue) error {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		klog.V(l4).InfoS(err.Error(), "component", "controller")

		return fmt.Errorf("node patch marshaling failure: %w", err)
	}

	_, err = enforcer.KubeClient.CoreV1().Nodes().Patch(
		context.TODO(), nodeName, types.JSONPatchType, jsonPayload, metav1.PatchOptions{})
	if err != nil {
		klog.V(l4).InfoS(err.Error(), "component", "controller")

		return fmt.Errorf("node patch failure: %w", err)
	}

	return nil
}

// appendViolationPatchValue appends a patch for either replacing a changed label or for adding a new one, if one
// doesn't already exist. Returns the given payload slice appended by any needed patch value.
// if label exists and the value has not changed, nothing is appended.
func appendViolationPatchValue(payload []patchValue, label string, node *v1.Node) ([]patchValue, error) {
	nameValuePair := strings.Split(label, "=")
	if len(nameValuePair) != pairValue {
		return payload, fmt.Errorf("invalid label, patch creation failed for: %v %w", label, errNull)
	}

	labelName := nameValuePair[0]
	oldValue, old := node.Labels[labelName]
	op := "add"

	if old {
		op = "replace"
	}

	if labelValue := nameValuePair[1]; !old || oldValue != labelValue {
		name := strings.ReplaceAll(labelName, "/", "~1")
		klog.V(l2).InfoS("patching for violation",
			"old", old, "op", op, "name", name, "labelValue", labelValue, "oldValue", oldValue)

		payload = append(payload, *createLabelPatchValue(op, name, labelValue))
	}

	return payload, nil
}

func appendNodeLabelCleanups(payload []patchValue, nodeViolatedLabels map[string]bool, node *v1.Node) []patchValue {
	for labelName, labelValue := range node.Labels {
		isViolatedLabel := nodeViolatedLabels[labelName+"="+labelValue]
		if strings.HasPrefix(labelName, labelPrefix) && !isViolatedLabel {
			name := strings.ReplaceAll(labelName, "/", "~1")
			klog.V(l2).InfoS("patching for cleanup", "name", name)
			payload = append(payload, *createLabelPatchValue("remove", name, ""))
		}
	}

	return payload
}

// updateNodeLabels takes the list of nodes violating the strategy. It then sets the payloads for labelling
// them as violators and calls for them to be labelled.
func (d *Strategy) updateNodeLabels(enforcer *strategy.MetricEnforcer,
	violations violationMap, allNodesViolatedLabels nodeViolations, allNodes *v1.NodeList) (int, error) {
	totalViolations := 0
	labelErrs := ""

	var errOut error

	for _, node := range allNodes.Items {
		node := node
		payload := []patchValue{}
		violatedPolicies := ""

		for policyName, labels := range violations[node.Name] {
			for _, label := range labels {
				payload, errOut = appendViolationPatchValue(payload, label, &node)
			}

			if errOut != nil {
				return -1, errOut
			}

			violatedPolicies += policyName + ", "
			totalViolations++
		}

		payload = appendNodeLabelCleanups(payload, allNodesViolatedLabels[node.Name], &node)

		if len(payload) > 0 {
			klog.V(l2).InfoS("Patching", "Payload:", payload)

			err := d.patchNode(node.Name, enforcer, payload)
			if err != nil {
				klog.V(l4).InfoS(err.Error(), "component", "controller")

				labelErrs = labelErrs + node.Name + ": [ " + violatedPolicies + " ]; "
			}
		}

		if len(violatedPolicies) > 0 {
			klog.V(l2).InfoS("Node "+node.Name+" violating "+violatedPolicies, "component", "controller")
		}
	}

	if len(labelErrs) > 0 {
		errOut = fmt.Errorf("could not label %v %w", labelErrs, errNull)
	}

	return totalViolations, errOut
}

// Function  will choose the biggest/lowest value depending on the rule operator.
func shouldUpdateRuleThreshold(result, olderRes ruleResult) bool {
	return ((result.rule.Operator == "GreaterThan" && result.quantity.Cmp(olderRes.quantity) > 0) ||
		(result.rule.Operator == "LessThan" && result.quantity.Cmp(olderRes.quantity) < 0))
}

// minMaxFilterViolatedRules filters out violated rules in case the same label name is being used.
// When the name is equal, only the largest or smallest value having metric among the rules will be
// returned in the result map, depending on the operator of the rule.
func minMaxFilterViolatedRules(violationResult interface{}) map[string]ruleResult {
	violatedRules := map[string]ruleResult{}

	defer func() {
		err := recover()
		if err != nil {
			klog.Error("Unsupported config: With equivalent labels, the operator in related rules must also be equivalent.")
		}
	}()

	for _, result := range violationResult.(*violationResultType).ruleResults {
		for _, label := range result.rule.Labels {
			nameValuePair := strings.Split(label, "=")
			name := nameValuePair[0]
			olderRes, old := violatedRules[name]

			if old && olderRes.rule.Operator != result.rule.Operator {
				log.Panic()
			}

			if !old || old && shouldUpdateRuleThreshold(result, olderRes) {
				violatedRules[name] = result
			}
		}
	}

	return violatedRules
}

// createLabels fills in labels to the given per-policy violation map and the flat map of all violated labels.
func createLabels(violatedRules map[string]ruleResult,
	nodeName, policyName string, violations violationMap, allViolatedLabels nodeViolations) {
	for _, result := range violatedRules {
		for _, label := range result.rule.Labels {
			violations[nodeName][policyName] = append(violations[nodeName][policyName], getPrefix(policyName)+label)
			allViolatedLabels[nodeName][getPrefix(policyName)+label] = true
		}
	}
}

// nodeStatusForStrategy returns violations as a slice of nodeName->policyName->[]label and
// as a flat map of the nodeName->label->true for quick access searching.
// Within same policy, overlapping label key values will go through min-max filtering, largest or smallest
// value producing metric will get its label depending on rule operator. Unique label keys will always be
// returned for the violating cases.
func (d *Strategy) nodeStatusForStrategy(enforcer *strategy.MetricEnforcer,
	cache cache.Reader) (violationMap, nodeViolations) {
	violations := violationMap{}
	allViolatedLabels := nodeViolations{}

	for strategy := range enforcer.RegisteredStrategies[StrategyType] {
		policyName := strategy.GetPolicyName()
		klog.V(l2).InfoS("Evaluating "+policyName, "component", "controller")

		nodes := strategy.Violated(cache)

		for nodeName, violationResult := range nodes {
			if _, ok := violations[nodeName]; !ok {
				violations[nodeName] = map[string][]string{}
			}

			if _, ok := allViolatedLabels[nodeName]; !ok {
				allViolatedLabels[nodeName] = map[string]bool{}
			}

			violatedRules := minMaxFilterViolatedRules(violationResult)
			createLabels(violatedRules, nodeName, policyName, violations, allViolatedLabels)
		}
	}

	return violations, allViolatedLabels
}

// Cleanup remove node labels for violating when policy is deleted.
func (d *Strategy) Cleanup(enforcer *strategy.MetricEnforcer, policyName string) error {
	nodes, err := enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		msg := fmt.Sprintf("cannot list nodes: %v", err)
		klog.V(l2).InfoS(msg, "component", "controller")

		return fmt.Errorf("Cleanup failure: %w", err)
	}

	for _, node := range nodes.Items {
		for labelName := range node.Labels {
			name := strings.ReplaceAll(labelName, "/", "~1")

			var payload []patchValue
			if strings.HasPrefix(labelName, getPrefix(policyName)) {
				payload = append(payload, *createLabelPatchValue("remove", name, ""))
			}

			err := d.patchNode(node.Name, enforcer, payload)
			if err != nil {
				klog.V(l2).InfoS(err.Error(), "component", "controller")
			}
		}
	}

	klog.V(l2).InfoS(fmt.Sprintf("Remove the node label on policy %v deletion", policyName), "component", "controller")

	return nil
}
