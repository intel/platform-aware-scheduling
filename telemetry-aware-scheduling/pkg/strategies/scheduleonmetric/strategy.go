// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// This is a semi-implemented strategy created to type scheduleonmetric as a strategy.

package scheduleonmetric

import (
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	telemetryPolicyV1 "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
)

// Strategy represents the TAS policy strategies.
type Strategy telemetryPolicyV1.TASPolicyStrategy

// StrategyType is set to schedule.
const (
	StrategyType = "scheduleonmetric"
)

// Violated is unimplemented for this strategy.
func (d *Strategy) Violated(_ cache.Reader) map[string]interface{} {
	violatingNodes := map[string]interface{}{}

	return violatingNodes
}

// Enforce is unimplemented.
func (d *Strategy) Enforce(_ *core.MetricEnforcer, _ cache.Reader) (int, error) {
	return 0, nil
}

// StrategyType returns the constant name of the strategy used to index it for other objects.
func (d *Strategy) StrategyType() string {
	return StrategyType
}

// Equals checks if this strategy shares a policy name and all rules with another strategy.
// This (like the equal method under the other strategy, is a naive implementation which could be expanded.
func (d *Strategy) Equals(other core.Interface) bool {
	otherScheduleOnMetricStrategy, ok := other.(*Strategy)
	sameName := other.GetPolicyName() == d.GetPolicyName()

	if ok && sameName && len(d.Rules) > 0 && len(d.Rules) == len(otherScheduleOnMetricStrategy.Rules) {
		for i, rule := range d.Rules {
			if rule.Metricname != otherScheduleOnMetricStrategy.Rules[i].Metricname {
				return false
			}

			if rule.Target != otherScheduleOnMetricStrategy.Rules[i].Target {
				return false
			}

			if rule.Operator != otherScheduleOnMetricStrategy.Rules[i].Operator {
				return false
			}
		}

		return true
	}

	return false
}

// GetPolicyName returns the policy name associated with this strategy.
func (d *Strategy) GetPolicyName() string {
	return d.PolicyName
}

// SetPolicyName sets the policy name for this strategy.
func (d *Strategy) SetPolicyName(policyName string) {
	d.PolicyName = policyName
}
