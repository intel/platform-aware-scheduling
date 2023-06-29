// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package labeling provides the labeling strategy. Violation conditions and enforcement behavior are defined here.
// When a node is violating the labeling strategy, the enforcer labels it as violating according to the policy label.
// This label can then be used externally to act on the strategy violation.
package labeling

import (
	"fmt"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	telempol "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

// StrategyType is set to "labeling".
const (
	StrategyType = "labeling"
	l2           = 2
	l4           = 4
)

// Strategy type for labeling from a single policy.
type Strategy telempol.TASPolicyStrategy

// StrategyType returns the name of the strategy type. This is used to place it in the registry.
func (d *Strategy) StrategyType() string {
	return StrategyType
}

type ruleResult struct {
	rule     telempol.TASPolicyRule
	quantity resource.Quantity
}

type violationResultType struct {
	ruleResults []ruleResult
}

func (d *Strategy) fetchRuleViolatingNodes(cache cache.Reader) map[string]interface{} {
	violatingNodes := map[string]interface{}{}

	for _, rule := range d.Rules {
		nodeMetrics, err := cache.ReadMetric(rule.Metricname)
		if err != nil {
			klog.V(l2).InfoS(err.Error(), "component", "controller")

			continue
		}

		for nodeName, nodeMetric := range nodeMetrics {
			msg := fmt.Sprint(nodeName+" "+rule.Metricname, " = ", nodeMetric.Value.AsDec())
			klog.V(l4).InfoS(msg, "component", "controller")

			if core.EvaluateRule(nodeMetric.Value, rule) {
				msg := fmt.Sprintf(nodeName + " violating " + d.PolicyName + ": " + ruleToString(rule))
				klog.V(l2).InfoS(msg, "component", "controller")

				if _, ok := violatingNodes[nodeName]; !ok {
					violatingNodes[nodeName] = &violationResultType{}
				}

				res, ok := violatingNodes[nodeName].(*violationResultType)
				if !ok {
					klog.Error("unexpected type")

					continue
				}

				res.ruleResults = append(res.ruleResults, ruleResult{rule: rule, quantity: nodeMetric.Value})
				if len(res.ruleResults) > 0 {
					for _, ruleRes := range res.ruleResults {
						klog.V(l2).Infof("Violated rules: %v", ruleToString(ruleRes.rule))
					}
				}
			}
		}
	}

	return violatingNodes
}

func (d *Strategy) filterViolatingNodeForAllLogicalOperator(violatingNodes map[string]interface{}) {
	if d.LogicalOperator == "allOf" {
		for nodeName := range violatingNodes {
			if _, ok := violatingNodes[nodeName]; ok {
				if res, ok := violatingNodes[nodeName].(*violationResultType); ok {
					if len(res.ruleResults) != len(d.Rules) {
						delete(violatingNodes, nodeName)
					}
				}
			}
		}
	}
}

// Violated checks if the strategy is violated by searching for nodes that have metrics that don't accord with
// the target in labeling strategy.
// Returns a map of nodeNames as key with a slice of violated rules and metric quantities in the result type.
func (d *Strategy) Violated(cache cache.Reader) map[string]interface{} {
	violatingNodes := d.fetchRuleViolatingNodes(cache)
	d.filterViolatingNodeForAllLogicalOperator(violatingNodes)

	return violatingNodes
}

// ruleToString returns the rule passed to it as a single string.
func ruleToString(rule telempol.TASPolicyRule) string {
	return fmt.Sprintf("%v %v %v %v", rule.Metricname, rule.Operator, rule.Target, rule.Labels)
}

func equalRules(a, b *telempol.TASPolicyRule) bool {
	if a.Metricname != b.Metricname {
		return false
	}

	if a.Target != b.Target {
		return false
	}

	if a.Operator != b.Operator {
		return false
	}

	for j := range a.Labels {
		if a.Labels[j] != b.Labels[j] {
			return false
		}
	}

	return true
}

// Equals checks if a strategy is the same as the passed strategy.
// It can be used to prevent duplication of strategies in the API and is also used to find strategies for deletion.
// TODO: Remedial action if equal, i.e. point to other strategies. Make method order ambivalent.
func (d *Strategy) Equals(other core.Interface) bool {
	otherLabelingStrategy, ok := other.(*Strategy)

	sameName := other.GetPolicyName() == d.GetPolicyName()
	if ok && sameName && len(d.Rules) > 0 && len(d.Rules) == len(otherLabelingStrategy.Rules) {
		for i, rule := range d.Rules {
			rule := rule
			if !equalRules(&rule, &otherLabelingStrategy.Rules[i]) {
				return false
			}
		}

		return true
	}

	return false
}

// GetPolicyName returns the name of the policy that originated strategy.
func (d *Strategy) GetPolicyName() string {
	return d.PolicyName
}

// SetPolicyName adds a policy name to be associated with this strategy.
func (d *Strategy) SetPolicyName(name string) {
	d.PolicyName = name
}
