// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package dontschedule

import (
	"reflect"
	"testing"
	"time"

	"k8s.io/klog/v2"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	v1 "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestDontScheduleStrategy_Violated(t *testing.T) {
	type args struct {
		cache cache.ReaderWriter
	}

	tests := []struct {
		name string
		d    Strategy
		args args
		want map[string]interface{}
	}{
		{name: "One node violating",
			d:    strategyRuleDefault("test name", "memory", "GreaterThan", 9),
			args: args{cache: cache.MockEmptySelfUpdatingCache()}, want: map[string]interface{}{"node-1": nil}},
		{name: "No nodes violating",
			d:    strategyRuleDefault("test name", "memory", "GreaterThan", 11),
			args: args{cache: cache.MockEmptySelfUpdatingCache()}, want: map[string]interface{}{}},
		{name: "No metric found",
			d:    strategyRuleDefault("test name", "mem", "GreaterThan", 9),
			args: args{cache: cache.MockEmptySelfUpdatingCache()}, want: map[string]interface{}{}},
		{name: "One node violating w/ a blank logical operator",
			d:    strategyRule("test-logic-1", "", "memory", "GreaterThan", 9),
			args: args{cache: cache.MockEmptySelfUpdatingCache()}, want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating w/ anyOf",
			d:    strategyRule("test-logic-2", "anyOf", "memory", "GreaterThan", 9),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating w/ allOf",
			d:    strategyRule("test-logic-3", "allOf", "memory", "GreaterThan", 9),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "no metric w/ blank logic operator",
			d:    strategyRule("test-logic-4", "", "mem", "GreaterThan", 9),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "no metric w/ anyOf",
			d:    strategyRule("test-logic-5", "anyOf", "mem", "GreaterThan", 9),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "no metric w/ allOf",
			d:    strategyRule("test-logic-6", "allOf", "mem", "GreaterThan", 9),
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the 1st rule w/o logical operator",
			d: Strategy{PolicyName: "test-logic-7", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu", "GreaterThan", 900)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating the 2nd rule w/o logical operator",
			d: Strategy{PolicyName: "test-logic-8", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90),
				metricRules("cpu", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating the 1st and 2nd rules w/o logical operator",
			d: Strategy{PolicyName: "test-logic-9", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating the 1st and no metric found for 2nd w/o logical operator",
			d: Strategy{PolicyName: "test-logic-10", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu-x", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "No node violating without logical operator",
			d: Strategy{PolicyName: "test-logic-11", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90),
				metricRules("cpu", "GreaterThan", 900)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the first rule w/ blank logical operator",
			d: Strategy{PolicyName: "test-logic-12", LogicalOperator: "", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu", "GreaterThan", 900)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating the second rule w/ blank logical operator",
			d: Strategy{PolicyName: "test-logic-13", LogicalOperator: "", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90),
				metricRules("cpu", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating the 1s and 2nd rules w/ blank logical operator",
			d: Strategy{PolicyName: "test-logic-14", LogicalOperator: "", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating the 1st and no metric found for 2nd w/ blank logical operator",
			d: Strategy{PolicyName: "test-logic-15", LogicalOperator: "", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu-x", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "No nodes violating w/ blank logical operator",
			d: Strategy{PolicyName: "test-logic-16", LogicalOperator: "", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90),
				metricRules("cpu", "GreaterThan", 900)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the first rule with anyOf",
			d: Strategy{PolicyName: "test-logic-17", LogicalOperator: "anyOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu", "GreaterThan", 900)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating the second rule with anyOf",
			d: Strategy{PolicyName: "test-logic-18", LogicalOperator: "anyOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90),
				metricRules("cpu", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating the 1s and 2nd rules with anyOf",
			d: Strategy{PolicyName: "test-logic-19", LogicalOperator: "anyOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating the 1st and no metric found for 2nd w/ anyOf",
			d: Strategy{PolicyName: "test-logic-20", LogicalOperator: "anyOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu-x", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "No nodes violating with anyOf",
			d: Strategy{PolicyName: "test-logic-21", LogicalOperator: "anyOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90),
				metricRules("cpu", "GreaterThan", 900)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the first rule with allOf",
			d: Strategy{PolicyName: "test-logic-22", LogicalOperator: "allOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu", "GreaterThan", 900)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the second rule with allOf",
			d: Strategy{PolicyName: "test-logic-23", LogicalOperator: "allOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90),
				metricRules("cpu", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "One node violating the 1s and 2nd rules with allOf",
			d: Strategy{PolicyName: "test-logic-24", LogicalOperator: "allOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{"node-1": nil}},
		{name: "One node violating the 1st and no metric found for 2nd w/ anyOf",
			d: Strategy{PolicyName: "test-logic-25", LogicalOperator: "allOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 9),
				metricRules("cpu-x", "GreaterThan", 90)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
		{name: "No nodes violating with allOf",
			d: Strategy{PolicyName: "test-logic-26", LogicalOperator: "allOf", Rules: []v1.TASPolicyRule{
				metricRules("memory", "GreaterThan", 90),
				metricRules("cpu", "GreaterThan", 900)}},
			args: args{cache: cache.MockEmptySelfUpdatingCache()},
			want: map[string]interface{}{}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tt.args.cache.WriteMetric("memory", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1,
				Value: *resource.NewQuantity(10, resource.DecimalSI)}})
			if err != nil {
				klog.InfoS(err.Error(), "component", "testing")
				klog.Exit(err)
			}
			err = tt.args.cache.WriteMetric("cpu", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1,
				Value: *resource.NewQuantity(200, resource.DecimalSI)}})
			if err != nil {
				t.Errorf("Cannot write metric to mock cach for test: %v", err)
			}
			if got := tt.d.Violated(tt.args.cache); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Strategy.Violated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func strategyRuleDefault(policyname, metricname, operator string, target int64) Strategy {
	return Strategy{
		PolicyName: policyname,
		Rules: []v1.TASPolicyRule{
			metricRules(metricname, operator, target)}}
}

func strategyRule(policyname, logicalOp, metricname, operator string, target int64) Strategy {
	return Strategy{
		PolicyName:      policyname,
		LogicalOperator: logicalOp,
		Rules: []v1.TASPolicyRule{
			metricRules(metricname, operator, target)}}
}

func metricRules(metricname string, operator string, target int64) v1.TASPolicyRule {
	return v1.TASPolicyRule{
		Metricname: metricname,
		Operator:   operator,
		Target:     target,
	}
}

func TestDontScheduleStrategy_StrategyType(t *testing.T) {
	tests := []struct {
		name string
		d    Strategy
		want string
	}{
		{"get strategy type", Strategy{}, "dontschedule"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			d := Strategy{}
			if got := d.StrategyType(); got != tt.want {
				t.Errorf("Strategy.StrategyType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDontScheduleStrategy_Equals(t *testing.T) {
	type args struct {
		in0 core.Interface
	}

	tests := []struct {
		name string
		d    Strategy
		args args
		want bool
	}{
		{"simple equality test ", Strategy{}, args{&Strategy{}}, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			d := Strategy{}
			if got := d.Equals(tt.args.in0); got != tt.want {
				t.Errorf("Strategy.Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDontScheduleStrategy_GetPolicyName(t *testing.T) {
	tests := []struct {
		name string
		d    Strategy
		want string
	}{
		{"get name", Strategy{}, "demo-policy"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			d := Strategy{}
			d.SetPolicyName("demo-policy")
			if got := d.GetPolicyName(); got != tt.want {
				t.Errorf("Strategy.GetPolicyName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDontScheduleStrategy_Enforce(t *testing.T) {
	type args struct {
		enforcer *core.MetricEnforcer
		cache    cache.ReaderWriter
	}

	tests := []struct {
		name    string
		d       Strategy
		args    args
		want    int
		wantErr bool
	}{
		{"simple enforce test", Strategy{}, args{&core.MetricEnforcer{}, &cache.AutoUpdatingCache{}}, 0, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			d := Strategy{}
			got, err := d.Enforce(tt.args.enforcer, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("Strategy.Enforce() error = %v, wantErr %v", err, tt.wantErr)

				return
			}
			if got != tt.want {
				t.Errorf("Strategy.Enforce() = %v, want %v", got, tt.want)
			}
		})
	}
}
