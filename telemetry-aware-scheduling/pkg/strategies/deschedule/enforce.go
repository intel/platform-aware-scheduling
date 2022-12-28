// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package deschedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	strategy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

const (
	l2                         = 2
	l4                         = 4
	failNodeListCleanUpMessage = "failed to list nodes during clean-up"
	failNodeListEnforceMessage = "failed to list all nodes during enforce"
	failNodePatchMessage       = "failed to patch node"
	failedLabelingMessage      = "could not label"
	defaultPolicyValue         = "violating"
)

var errNull = errors.New("")

type violationList map[string][]string

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

// Cleanup remove node labels for violating when policy is deleted.
func (d *Strategy) Cleanup(enforcer *strategy.MetricEnforcer, policyName string) error {
	lbls := metav1.LabelSelector{MatchLabels: map[string]string{policyName: "violating"}}

	nodes, err := enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: labels.Set(lbls.MatchLabels).String()})
	if err != nil {
		msg := fmt.Sprintf("cannot list nodes: %v", err)
		klog.V(l2).InfoS(msg, "component", "controller")

		return fmt.Errorf("%s: %w", failNodeListCleanUpMessage, err)
	}

	for _, node := range nodes.Items {
		var payload []patchValue

		if _, ok := node.Labels[policyName]; ok {
			msg := fmt.Sprintf("patch %s label for removal with empty value", policyName)
			klog.V(l2).InfoS(msg, "component", "controller")

			payload = append(payload, *createLabelPatchValue("remove", policyName, ""))
		}

		err := d.patchNode(node.Name, enforcer, payload)
		if err != nil {
			klog.V(l2).InfoS(err.Error(), "component", "controller")
		}
	}

	klog.V(l2).InfoS(fmt.Sprintf("Remove the node label on policy %v deletion", policyName), "component", "controller")

	return nil
}

// Enforce describes the behavior followed by this strategy to return associated pods to non-violating status.
// For descheduling enforcement is done by labelling the nodes as violators. This label can then be used externally,
// for example by descheduler, to remedy the situation. Here we make an api call to list all nodes first.
// This may be improved by using a controller instead or some other way of not waiting for the API call every time Enforce is called.
func (d *Strategy) Enforce(enforcer *strategy.MetricEnforcer, cache cache.Reader) (int, error) {
	nodes, err := enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		msg := fmt.Sprintf("cannot list nodes: %v", err)
		klog.V(l2).InfoS(msg, "component", "controller")

		return -1, fmt.Errorf("%s: %w", failNodeListEnforceMessage, err)
	}

	list := d.nodeStatusForStrategy(enforcer, cache)

	numberViolations, err := d.updateNodeLabels(enforcer, list, nodes)
	if err != nil {
		klog.V(l2).InfoS(err.Error(), "component", "controller")

		return -1, fmt.Errorf("failed to get violation list: %w", err)
	}

	return numberViolations, nil
}

// patchNode takes a json patch value and sends it to the API server to patch a node. Here it's used to label nodes.
func (d *Strategy) patchNode(nodeName string, enforcer *strategy.MetricEnforcer, payload []patchValue) error {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		klog.V(l4).InfoS(err.Error(), "component", "controller")

		return fmt.Errorf("fail to encode patch %v to JSON: %w", payload, err)
	}

	_, err = enforcer.KubeClient.CoreV1().Nodes().Patch(context.TODO(), nodeName, types.JSONPatchType, jsonPayload, metav1.PatchOptions{})
	if err != nil {
		klog.V(l4).InfoS(err.Error(), "component", "controller")

		return fmt.Errorf("%s with %v: %w", failNodePatchMessage, payload, err)
	}

	return nil
}

// allPolicies returns a list the set of all policies registered with the enforcer.
func allPolicies(enforcer *strategy.MetricEnforcer) map[string]interface{} {
	policies := map[string]interface{}{}
	for k := range enforcer.RegisteredStrategies[StrategyType] {
		policies[k.GetPolicyName()] = nil
	}

	return policies
}

// appendViolationPatchValue appends a de-scheduling patch to a node if it doesn't already exist.
// It returns the given payload appended by any patch value.
func appendViolationPatchValue(payload []patchValue, policyName string, node v1.Node) []patchValue {
	labelValue, ok := node.Labels[policyName]

	if !ok || (ok && labelValue != defaultPolicyValue) {
		msg := fmt.Sprintf("patching for violation %s with value %s", policyName, defaultPolicyValue)
		klog.V(l2).InfoS(msg, "component", "controller")

		payload = append(payload, *createLabelPatchValue("add", policyName, defaultPolicyValue))
	}

	return payload
}

// updateNodeLabels takes the list of nodes violating the strategy.
// It then sets the payloads for labelling them as violators and calls for them to be labelled.
func (d *Strategy) updateNodeLabels(enforcer *strategy.MetricEnforcer, viols violationList, allNodes *v1.NodeList) (int, error) {
	totalViolations := 0
	labelErrs := ""

	var errOut error

	var nonViolatedPolicies map[string]interface{}

	for _, node := range allNodes.Items {
		var payload []patchValue

		nonViolatedPolicies = allPolicies(enforcer)
		violatedPolicies := ""

		for _, policyName := range viols[node.Name] {
			delete(nonViolatedPolicies, policyName)

			payload = appendViolationPatchValue(payload, policyName, node)
			violatedPolicies += policyName + ", "
		}

		for policyName := range nonViolatedPolicies {
			if _, ok := node.Labels[policyName]; ok {
				klog.V(l2).InfoS("patching for removal", "name", policyName,
					"labelValue", "")

				payload = append(payload, *createLabelPatchValue("remove", policyName, ""))
			}
			totalViolations++
		}

		if len(payload) != 0 {
			err := d.patchNode(node.Name, enforcer, payload)

			if err != nil {
				if len(labelErrs) == 0 {
					labelErrs = "could not label: "
				}

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

// nodeStatusForStrategy returns a list of nodes that are violating the given strategy by calling the strategies Violated method.
func (d *Strategy) nodeStatusForStrategy(enforcer *strategy.MetricEnforcer, cache cache.Reader) violationList {
	violations := violationList{}

	for strg := range enforcer.RegisteredStrategies[StrategyType] {
		klog.V(l2).InfoS("Evaluating "+strg.GetPolicyName(), "component", "controller")
		nodes := strg.Violated(cache)

		for node := range nodes {
			violations[node] = append(violations[node], strg.GetPolicyName())
		}
	}

	return violations
}
