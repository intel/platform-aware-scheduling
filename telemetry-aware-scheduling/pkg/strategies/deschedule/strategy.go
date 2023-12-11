// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package deschedule provides the deschedule strategy. Violation conditions and enforcement behavior are defined here.
// When a node is violating the deschedule strategy, the enforcer labels it as violating.
// This label can then be used externally to act on the strategy violation.
package deschedule

import (
	"fmt"

	"k8s.io/klog/v2"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	telempol "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
)

// StrategyType is set to de-schedule.
const (
	StrategyType = "deschedule"
)

// Strategy type for de-scheduling from a single policy.
type Strategy telempol.TASPolicyStrategy

// StrategyType returns the name of the strategy type. This is used to place it in the registry.
func (d *Strategy) StrategyType() string {
	return StrategyType
}

// Violated checks to see if the strategy is violated by searching for nodes that have metrics that don't accord with the target in descheduling strategy.
// Returns a map of nodeNames as key with an empty value associated with each.
func (d *Strategy) Violated(cache cache.Reader) map[string]interface{} {
	violatingNodes := map[string]interface{}{}
	nodeMetricViol := map[string]int{}

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
				klog.V(l2).Infof("%v violated in node %v", rule.Metricname, nodeName)
				nodeMetricViol[nodeName]++

				if d.LogicalOperator == "allOf" {
					if nodeMetricViol[nodeName] == len(d.Rules) {
						msg := nodeName + " violating all the rules in " + d.StrategyType() + " strategy"
						klog.V(l2).InfoS(msg, "component", "controller")

						violatingNodes[nodeName] = nil
					}
				} else {
					msg := fmt.Sprintf(nodeName + " violating " + d.PolicyName + ": " + ruleToString(rule))
					klog.V(l2).InfoS(msg, "component", "controller")

					violatingNodes[nodeName] = nil
				}
			}
		}
	}

	return violatingNodes
}

// ruleToString returns the rule passed to it as a single string.
func ruleToString(rule telempol.TASPolicyRule) string {
	return fmt.Sprintf("%v %v %v", rule.Metricname, rule.Operator, rule.Target)
}

// Equals checks if a strategy is the same as the passed strategy.
// It can be used to prevent duplication of strategies in the API and is also used to find strategies for deletion.
// TODO: Remedial action if equal, i.e. point to other strategies. Make method order ambivalent.
func (d *Strategy) Equals(other core.Interface) bool {
	otherDeschedulerStrategy, ok := other.(*Strategy)
	sameName := other.GetPolicyName() == d.GetPolicyName()

	if ok && sameName && len(d.Rules) > 0 && len(d.Rules) == len(otherDeschedulerStrategy.Rules) {
		for i, rule := range d.Rules {
			if rule.Metricname != otherDeschedulerStrategy.Rules[i].Metricname {
				return false
			}

			if rule.Target != otherDeschedulerStrategy.Rules[i].Target {
				return false
			}

			if rule.Operator != otherDeschedulerStrategy.Rules[i].Operator {
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
