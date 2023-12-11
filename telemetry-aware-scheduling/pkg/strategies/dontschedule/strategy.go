// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// This is a semi-implemented strategy created to type dontschedule as a strategy.

package dontschedule

import (
	"fmt"

	"k8s.io/klog/v2"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	telemetryPolicyV1 "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
)

// Strategy represents the TAS policy strategies.
type Strategy telemetryPolicyV1.TASPolicyStrategy

// StrategyType is set to not schedule.
const (
	StrategyType = "dontschedule"
	l2           = 2
)

// Violated compares the list of rules against the metric values pulled from the cache.
// If any single rule is violated the method returns a set of nodes that are currently in violation.
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
			klog.V(l2).InfoS(msg, "component", "controller")

			if core.EvaluateRule(nodeMetric.Value, rule) {
				nodeMetricViol[nodeName]++

				if d.LogicalOperator == "allOf" {
					if nodeMetricViol[nodeName] == len(d.Rules) {
						msg := nodeName + " violating all the rules in " + d.StrategyType() + " strategy"
						klog.V(l2).InfoS(msg, "component", "controller")

						violatingNodes[nodeName] = nil
					}
				} else {
					msg := nodeName + " violating " + d.PolicyName + ": " + ruleToString(rule)
					klog.V(l2).InfoS(msg, "component", "controller")

					violatingNodes[nodeName] = nil
				}
			}
		}
	}

	return violatingNodes
}

// Enforce unimplemented for dontschedule.
func (d *Strategy) Enforce(_ *core.MetricEnforcer, _ cache.Reader) (int, error) {
	return 0, nil
}

// StrategyType returns the strategy type constant.
func (d *Strategy) StrategyType() string {
	return StrategyType
}

// Equals implementation which checks to see if all rules and the policy name are equal for this strategy and another.
// Used to avoid duplications and to find the correct strategy for deletions in the index.
func (d *Strategy) Equals(other core.Interface) bool {
	OtherDontScheduleStrategy, ok := other.(*Strategy)
	sameName := other.GetPolicyName() == d.GetPolicyName()

	if ok && sameName && len(d.Rules) > 0 && len(d.Rules) == len(OtherDontScheduleStrategy.Rules) {
		for i, rule := range d.Rules {
			if rule.Metricname != OtherDontScheduleStrategy.Rules[i].Metricname {
				return false
			}

			if rule.Target != OtherDontScheduleStrategy.Rules[i].Target {
				return false
			}

			if rule.Operator != OtherDontScheduleStrategy.Rules[i].Operator {
				return false
			}
		}

		return true
	}

	return false
}

// SetPolicyName sets a connected policy name for this strategy.
func (d *Strategy) SetPolicyName(policyName string) {
	d.PolicyName = policyName
}

// GetPolicyName returns the set policy name for this strategy.
func (d *Strategy) GetPolicyName() string {
	return d.PolicyName
}

// Formats the rules as an interpretable string.
func ruleToString(rule telemetryPolicyV1.TASPolicyRule) string {
	return fmt.Sprintf("%v %v %v", rule.Metricname, rule.Operator, rule.Target)
}
