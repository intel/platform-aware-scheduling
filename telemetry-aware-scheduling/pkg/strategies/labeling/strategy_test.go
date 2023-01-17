// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The labeling strategy, violations conditions and enforcement behavior are defined in this package.
// When a node is violating the labeling strategy, the enforcer labels it by the label defined in the policy.
// This label can then be used externally to act on the strategy violation.
package labeling

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	v1 "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

func TestLabelingStrategy_SetPolicyName(t *testing.T) {
	klog.InfoS("entered in strategy", "component", "testing")

	type args struct {
		name string
	}

	tests := []struct {
		name string
		d    *Strategy
		args args
	}{
		{name: "set no name", d: &Strategy{}, args: args{}},
		{name: "set basic name", d: &Strategy{}, args: args{"test policy"}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tt.d.SetPolicyName(tt.args.name)
			if tt.d.PolicyName != tt.args.name {
				t.Error("Outcome didn't match expected result")
			}
		})
	}
}

func TestLabelingStrategy_GetPolicyName(t *testing.T) {
	tests := []struct {
		name string
		d    *Strategy
		want string
	}{
		{name: "retrieve basic name", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{}}, want: "test name"},
		{name: "retrieve no name", d: &Strategy{PolicyName: "", Rules: []v1.TASPolicyRule{}}, want: ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.GetPolicyName(); got != tt.want {
				t.Errorf("Strategy.GetPolicyName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLabelingStrategy_Equals(t *testing.T) {
	type args struct {
		other core.Interface
	}

	tests := []struct {
		name string
		d    *Strategy
		args args
		want bool
	}{
		{name: "Not Equal: one empty strategies",
			d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{}},
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{})}},
		{name: "Equal: one rule per strategy",
			d: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=true"}),
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=true"})},
			want: true},
		{name: "Equal: 2 different rules same order",
			d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{
				metricRules("cpu", "Equals", 1, []string{"card0=false"}),
				metricRules("memory", "GreaterThan", 50, []string{"card0=true"})}},
			args: args{
				other: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{
					metricRules("cpu", "Equals", 1, []string{"card0=false"}),
					metricRules("memory", "GreaterThan", 50, []string{"card0=true"})}}},
			want: true},
		{name: "Not equal: 2 different rules different order",
			d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 50, []string{"card0=true"}),
				metricRules("cpu", "Equals", 1, []string{"card0=false"})}},
			args: args{
				other: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{
					metricRules("cpu", "Equals", 1, []string{"card0=false"}),
					metricRules("memory", "GreaterThan", 50, []string{"card0=true"})}}},
			want: false},
		{name: "Not equal: different number rules",
			d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{
				metricRules("cpu", "Equals", 1, []string{"card0=false"}),
				metricRules("memory", "GreaterThan", 50, []string{"card0=true"})}},
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=true"})},
			want: false},
		{name: "Not equal: different number rules different order",
			d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 50, []string{}),
				metricRules("cpu", "Equals", 1, []string{"card0=false"})}},
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{})},
			want: false},
		{name: "Not equal: different rules",
			d: strategyRuleDefault("test name", "cpu", "GreaterThan", 50, []string{"card0=false"}),
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=true"})},
			want: false},
		{name: "Not equal: different operator",
			d: strategyRuleDefault("test name", "memory", "LessThan", 50, []string{"card0=false"}),
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=false"})},
			want: false},
		{name: "Not equal: different target",
			d: strategyRuleDefault("test name", "memory", "LessThan", 10, []string{"card0=false"}),
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=false"})},
			want: false},
		{name: "Not equal: different metrics",
			d: strategyRuleDefault("test name", "cpu", "GreaterThan", 50, []string{"card0=false"}),
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=false"})},
			want: false},
		{name: "Not equal: different labels",
			d: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=true"}),
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=false"})},
			want: false},
		{name: "Equal: 2 labels same order",
			d: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=true", "card1=false"}),
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=true", "card1=false"})},
			want: true},
		{name: "Not Equal: 2 labels different order",
			d: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card1=false", "card0=true"}),
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=true", "card1=false"})},
			want: false},
		{name: "Not Equal: different number of labels",
			d: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card1=false"}),
			args: args{
				other: strategyRuleDefault("test name", "memory", "GreaterThan", 50, []string{"card0=true", "card1=false"})},
			want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.Equals(tt.args.other); got != tt.want {
				a, _ := tt.args.other.(*Strategy)
				msg := fmt.Sprint(a)
				klog.InfoS(msg, "component", "testing")
				t.Errorf("Strategy.Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLabelingStrategy_Violated(t *testing.T) {
	type args struct {
		cache cache.ReaderWriter
	}

	tests := []struct {
		name string
		d    *Strategy
		args args
		want map[string]interface{}
	}{
		{name: "One node violating",
			d:    strategyRuleDefault("test name", "memory", "GreaterThan", 9, []string{"card0=false"}),
			args: args{cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"})}}},
		{name: "No nodes violating",
			d:    strategyRuleDefault("test name", "memory", "GreaterThan", 11, []string{"card0=false"}),
			args: args{cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "No metric found",
			d:    strategyRuleDefault("test name", "", "GreaterThan", 9, []string{"card0=false"}),
			args: args{cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "No labels found",
			d:    strategyRuleDefault("test name", "memory", "GreaterThan", 9, []string{}),
			args: args{cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{})}}},
		{name: "No metrics and labels found",
			d:    strategyRuleDefault("test name", "", "GreaterThan", 9, []string{}),
			args: args{cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating w/ a blank logical operator and one metric",
			d:    strategyRule("test-logic-1", "", "memory", "GreaterThan", 9, []string{"card0=false"}),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"})}}},
		{name: "One node violating w/ anyOf and one metric",
			d:    strategyRule("test-logic-2", "anyOf", "memory", "GreaterThan", 9, []string{"card0=false"}),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"})}}},
		{name: "One node violating w/ allOf and one metric",
			d:    strategyRule("test-logic-3", "allOf", "memory", "GreaterThan", 9, []string{"card0=false"}),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"})}}},
		{name: "no metric w/ blank logic operator and one metric",
			d:    strategyRule("test-logic-4", "", "mem", "GreaterThan", 9, []string{"card0=false"}),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "no metric w/ anyOf and one metric",
			d:    strategyRule("test-logic-5", "anyOf", "mem", "GreaterThan", 9, []string{"card0=false"}),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "no metric w/ allOf and one metric",
			d:    strategyRule("test-logic-6", "allOf", "mem", "GreaterThan", 9, []string{"card0=false"}),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the 1st rule w/o logical operator",
			d: &Strategy{PolicyName: "test-logic-7", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 900, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"})}}},
		{name: "One node violating the 2nd rule w/o logical operator",
			d: &Strategy{PolicyName: "test-logic-8", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}}},
		{name: "One node violating the 1st and 2nd rules w/o logical operator",
			d: &Strategy{PolicyName: "test-logic-9", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}}},
		{name: "One node violating the 1st and no metric found for 2nd w/o logical operator",
			d: &Strategy{PolicyName: "test-logic-10", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu-x", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"})}}},
		{name: "No node violating without logical operator",
			d: &Strategy{PolicyName: "test-logic-11", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 900, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the first rule w/ blank logical operator",
			d: &Strategy{PolicyName: "test-logic-12", LogicalOperator: "", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 900, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"})}}},
		{name: "One node violating the second rule w/ blank logical operator",
			d: &Strategy{PolicyName: "test-logic-13", LogicalOperator: "", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}}},
		{name: "One node violating the 1s and 2nd rules w/ blank logical operator",
			d: &Strategy{PolicyName: "test-logic-14", LogicalOperator: "", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}}},
		{name: "One node violating the 1st and no metric found for 2nd w/ blank logical operator",
			d: &Strategy{PolicyName: "test-logic-15", LogicalOperator: "", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu-x", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"})}}},
		{name: "No nodes violating w/ blank logical operator",
			d: &Strategy{PolicyName: "test-logic-16", LogicalOperator: "", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 900, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the first rule with anyOf",
			d: &Strategy{PolicyName: "test-logic-17", LogicalOperator: "anyOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 900, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"})}}},
		{name: "One node violating the second rule with anyOf",
			d: &Strategy{PolicyName: "test-logic-18", LogicalOperator: "anyOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}}},
		{name: "One node violating the 1s and 2nd rules with anyOf",
			d: &Strategy{PolicyName: "test-logic-19", LogicalOperator: "anyOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}}},
		{name: "One node violating the 1st and no metric found for 2nd w/ anyOf",
			d: &Strategy{PolicyName: "test-logic-20", LogicalOperator: "anyOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu-x", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"})}}},
		{name: "No nodes violating with anyOf",
			d: &Strategy{PolicyName: "test-logic-21", LogicalOperator: "anyOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 900, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the first rule with allOf",
			d: &Strategy{PolicyName: "test-logic-22", LogicalOperator: "allOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 900, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the second rule with allOf",
			d: &Strategy{PolicyName: "test-logic-23", LogicalOperator: "allOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the 1s and 2nd rules with allOf",
			d: &Strategy{PolicyName: "test-logic-24", LogicalOperator: "allOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 90, []string{"card1=true"})}}},
		{name: "One node violating the 1st and no metric found for 2nd w/ anyOf",
			d: &Strategy{PolicyName: "test-logic-25", LogicalOperator: "allOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9, []string{"card0=false"}),
				metricRules("cpu-x", "GreaterThan", 90, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "No nodes violating with allOf",
			d: &Strategy{PolicyName: "test-logic-26", LogicalOperator: "allOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90, []string{"card0=false"}),
				metricRules("cpu", "GreaterThan", 900, []string{"card1=true"})}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tt.args.cache.WriteMetric("memory", metrics.NodeMetricsInfo{
				"node-1": {Timestamp: time.Now(), Window: 1, Value: *resource.NewQuantity(10, resource.DecimalSI)}})
			if err != nil {
				klog.InfoS("testing metric write on cache failed"+err.Error(), "component", "testing")
			}
			err = tt.args.cache.WriteMetric("cpu", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1,
				Value: *resource.NewQuantity(200, resource.DecimalSI)}})
			if err != nil {
				t.Errorf("Cannot write metric to mock cache for test: %v", err)
			}
			tmp := map[string]interface{}{}
			var violRules []v1.TASPolicyRule
			for node, t1 := range tt.d.Violated(tt.args.cache) {
				for _, t2 := range t1.(*violationResultType).ruleResults {
					violRules = append(violRules, t2.rule)
				}
				tmp[node] = violRules
			}
			if got := tmp; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Strategy.Violated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func strategyRuleDefault(policyname, metricname, operator string, target int64, labels []string) *Strategy {
	return &Strategy{
		PolicyName: policyname,
		Rules: []v1.TASPolicyRule{
			metricRules(metricname, operator, target, labels)}}
}

func strategyRule(policyname, logicalOp, metricname, operator string, target int64, labels []string) *Strategy {
	return &Strategy{
		PolicyName:      policyname,
		LogicalOperator: logicalOp,
		Rules: []v1.TASPolicyRule{
			metricRules(metricname, operator, target, labels)}}
}

func metricRules(metricname string, operator string, target int64, labels []string) v1.TASPolicyRule {
	return v1.TASPolicyRule{
		Metricname: metricname,
		Operator:   operator,
		Target:     target,
		Labels:     labels,
	}
}

func TestLabelingStrategy_StrategyType(t *testing.T) {
	tests := []struct {
		name string
		d    *Strategy
		want string
	}{
		{name: "basic type", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{}}, want: "labeling"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.StrategyType(); got != tt.want {
				t.Errorf("Strategy.StrategyType() = %v, want %v", got, tt.want)
			}
		})
	}
}
