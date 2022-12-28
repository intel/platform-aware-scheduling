// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"reflect"
	"testing"
	"time"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	telemetrypolicy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func testNodeMetricCustomInfo(nodeNames []string, numbers []int64) metrics.NodeMetricsInfo {
	n := metrics.NodeMetricsInfo{}
	for i, name := range nodeNames {
		n[name] = metrics.NodeMetric{Value: *resource.NewQuantity(numbers[i], resource.DecimalSI), Window: time.Second, Timestamp: time.Now()}
	}

	return n
}

func TestOperator(t *testing.T) {
	type args struct {
		value int64
		rule  telemetrypolicy.TASPolicyRule
	}

	tests := []struct {
		name string
		args args
		want bool
	}{
		{name: "LessThan true",
			args: args{value: 100, rule: telemetrypolicy.TASPolicyRule{Metricname: "memory", Operator: "LessThan", Target: 1000}},
			want: true},
		{name: "GreaterThan true",
			args: args{value: 100000, rule: telemetrypolicy.TASPolicyRule{Metricname: "memory", Operator: "GreaterThan", Target: 1}},
			want: true},
		{name: "Equals true",
			args: args{value: 1, rule: telemetrypolicy.TASPolicyRule{Metricname: "memory", Operator: "Equals", Target: 1}},
			want: true},
		{name: "LessThan false",
			args: args{value: 10000, rule: telemetrypolicy.TASPolicyRule{Metricname: "memory", Operator: "LessThan", Target: 10}}},
		{name: "GreaterThan false",
			args: args{value: 1, rule: telemetrypolicy.TASPolicyRule{Metricname: "memory", Operator: "GreaterThan", Target: 10000}}},
		{name: "Equals false",
			args: args{value: 1, rule: telemetrypolicy.TASPolicyRule{Metricname: "memory", Operator: "Equals", Target: 100}}},
		{name: "Invalid Operator",
			args: args{value: 100, rule: telemetrypolicy.TASPolicyRule{Metricname: "memory", Operator: "ABCDE", Target: 1000}},
			want: false},
		{name: "Blank Operator",
			args: args{value: 100, rule: telemetrypolicy.TASPolicyRule{Metricname: "memory", Operator: "", Target: 1000}},
			want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			num := *resource.NewQuantity(tt.args.value, resource.DecimalSI)
			if got := EvaluateRule(num, tt.args.rule); got != tt.want {
				t.Errorf("EvaluateRule() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrderedList(t *testing.T) {
	type args struct {
		metricsInfo metrics.NodeMetricsInfo
		operator    string
	}

	tests := []struct {
		name string
		args args
		want []NodeSortableMetric
	}{
		{"less than test",
			args{testNodeMetricCustomInfo([]string{"node A", "node B", "node C"}, []int64{100, 200, 10}), "LessThan"},
			[]NodeSortableMetric{
				{"node C", *resource.NewQuantity(10, resource.DecimalSI)},
				{"node A", *resource.NewQuantity(100, resource.DecimalSI)},
				{"node B", *resource.NewQuantity(200, resource.DecimalSI)}}},
		{"greater than test",
			args{testNodeMetricCustomInfo([]string{"node A", "node B", "node C"}, []int64{100, 200, 10}), "GreaterThan"},
			[]NodeSortableMetric{
				{"node B", *resource.NewQuantity(200, resource.DecimalSI)},
				{"node A", *resource.NewQuantity(100, resource.DecimalSI)},
				{"node C", *resource.NewQuantity(10, resource.DecimalSI)}}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := OrderedList(tt.args.metricsInfo, tt.args.operator)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("OrderedList() = %v, want %v", got, tt.want)
			}
		},
		)
	}
}
