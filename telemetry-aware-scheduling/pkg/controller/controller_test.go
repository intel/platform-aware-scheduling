// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Provides a controller that can be used to watch policies in the Kuebrnetes API.
// It registers strategies from those policies to an enforcer.
package controller

import (
	"reflect"
	"testing"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	strategy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/deschedule"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/labeling"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	api "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

var (
	policy1 = getTASPolicy("policy1", "default", dontschedule.StrategyType, []api.TASPolicyRule{
		{Metricname: "filter1_metric", Operator: "LessThan", Target: 20, Labels: []string{}}})
	policy2 = getTASPolicy("policy2", "default", deschedule.StrategyType, []api.TASPolicyRule{
		{Metricname: "filter2_metric", Operator: "GreatThan", Target: 20, Labels: []string{}}})
	policy3 = getTASPolicy("policy3", "default", scheduleonmetric.StrategyType, []api.TASPolicyRule{
		{Metricname: "filter3_metric", Operator: "GreatThan", Target: 20, Labels: []string{}}})
	policy4 = getTASPolicy("policy4", "default", labeling.StrategyType, []api.TASPolicyRule{
		{Metricname: "filter4_metric", Operator: "Equals", Target: 20, Labels: []string{}}})
	policy5 = getTASPolicy("policy5", "default", "strategy_unavailable", []api.TASPolicyRule{
		{Metricname: "filter5_metric", Operator: "LessThan", Target: 20, Labels: []string{}}})
	policy6 = getTASPolicy("policy6", "default", dontschedule.StrategyType, []api.TASPolicyRule{
		{Metricname: "", Operator: "LessThan", Target: 20, Labels: []string{}}})
	policy7 = getTASPolicy("", "", dontschedule.StrategyType, []api.TASPolicyRule{
		{Metricname: "", Operator: "", Target: 0, Labels: []string{}}})
	policy8 = getTASPolicy("policy8", "not default", scheduleonmetric.StrategyType, []api.TASPolicyRule{
		{Metricname: "filter8_metric", Operator: "GreatThan", Target: 20, Labels: []string{}}})
)

func getTASPolicy(name, namespace string, str string, metricRule []api.TASPolicyRule) *api.TASPolicy {
	pol := &api.TASPolicy{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: api.TASPolicySpec{
			Strategies: map[string]api.TASPolicyStrategy{
				str: {
					PolicyName: name,
					Rules:      metricRule,
				},
			},
		},
	}

	return pol
}

func TestTelemetryPolicyController_onAdd(t *testing.T) {
	type interfaceMock struct {
		rest.Interface
	}

	type fields struct {
		Interface interfaceMock
		Writer    cache.MockCache
		Enforcer  strategy.MockStrategy
	}

	type args struct {
		obj interface{}
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		expect interface{}
		want   bool
	}{
		{
			name:   "policy with dontschedule strategy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy1},
			expect: &dontschedule.Strategy{PolicyName: "policy1", LogicalOperator: "",
				Rules: []api.TASPolicyRule{{Metricname: "filter1_metric", Operator: "LessThan", Target: 20,
					Labels: []string{}}}},
			want: true,
		},
		{
			name:   "policy with  deschedule strategy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy2},
			expect: &deschedule.Strategy{PolicyName: policy2.Spec.Strategies["deschedule"].PolicyName, LogicalOperator: "",
				Rules: []api.TASPolicyRule{{Metricname: "filter2_metric", Operator: "GreatThan", Target: 20,
					Labels: []string{}}}},
			want: true,
		},
		{
			name:   "policy with wrong strategy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy5},
			expect: nil,
			want:   false,
		},
		{
			name:   "No policy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{},
			expect: nil,
			want:   false,
		},
		{
			name:   "policy without name, namespace and metric rules",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy7},
			expect: &dontschedule.Strategy{PolicyName: "", LogicalOperator: "",
				Rules: []api.TASPolicyRule{{Metricname: "", Operator: "", Target: 0, Labels: []string{}}}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			controller := &TelemetryPolicyController{
				Interface: tt.fields.Interface,
				Writer:    tt.fields.Writer,
				Enforcer:  &tt.fields.Enforcer,
			}
			controller.onAdd(tt.args.obj)
			enforced := tt.fields.Enforcer
			gotStrMetricRule := enforced.AddedStrategies.I
			if gotStrMetricRule == nil {
				got = false
			} else {
				got = reflect.DeepEqual(tt.expect, gotStrMetricRule)
			}
			if got != tt.want {
				t.Errorf("Got %v, want %v", gotStrMetricRule, tt.expect)
			}
		})
	}
}

func TestTelemetryPolicyController_onDelete(t *testing.T) {
	type interfaceMock struct {
		rest.Interface
	}

	type fields struct {
		Interface interfaceMock
		Writer    cache.MockCache
		Enforcer  strategy.MockStrategy
	}

	type args struct {
		obj interface{}
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		expect interface{}
		want   bool
	}{
		{
			name:   "polycy with schedulonmetric strategy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy3},
			expect: nil,
			want:   true,
		},
		{
			name:   "policy with wrong strategy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy5},
			expect: nil,
			want:   true,
		},
		{
			name:   "policy in wrong namespace",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy8},
			expect: &scheduleonmetric.Strategy{PolicyName: "policy8", LogicalOperator: "",
				Rules: []api.TASPolicyRule{{Metricname: "filter8_metric", Operator: "GreatThan", Target: 20,
					Labels: []string{}}}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			controller := &TelemetryPolicyController{
				tt.fields.Interface,
				tt.fields.Writer,
				&tt.fields.Enforcer,
			}
			controller.onDelete(tt.args.obj)
			enforced := tt.fields.Enforcer
			gotStrMetricRule := enforced.RemovedStrategies
			got = reflect.DeepEqual(tt.expect, gotStrMetricRule)
			if got != tt.want {
				t.Errorf("Got %v, want %v", gotStrMetricRule, tt.expect)
			}
		})
	}
}

func TestTelemetryPolicyController_onUpdate(t *testing.T) {
	type interfaceMock struct {
		rest.Interface
	}

	type fields struct {
		Interface interfaceMock
		Writer    cache.MockCache
		Enforcer  strategy.MockStrategy
	}

	type args struct {
		old interface{}
		new interface{}
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		expect interface{}
		want   bool
	}{
		{
			name:   "policy with donschedule replaced by labeling strategy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy1, policy4},
			expect: &labeling.Strategy{PolicyName: "policy4", LogicalOperator: "",
				Rules: []api.TASPolicyRule{{Metricname: "filter4_metric", Operator: "Equals", Target: 20,
					Labels: []string{}}}},
			want: true,
		},
		{
			name:   "policy with labeling replaced by deschedule strategy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy4, policy2},
			expect: &deschedule.Strategy{PolicyName: policy2.Spec.Strategies["deschedule"].PolicyName, LogicalOperator: "",
				Rules: []api.TASPolicyRule{{Metricname: "filter2_metric", Operator: "GreatThan", Target: 20,
					Labels: []string{}}}},
			want: true,
		},
		{
			name:   "replace a policy with a policy with wrong strategy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy1, policy5},
			expect: nil,
			want:   false,
		},
		{
			name:   "replace the same policy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy1, policy1},
			expect: &dontschedule.Strategy{PolicyName: "policy1", LogicalOperator: "",
				Rules: []api.TASPolicyRule{{Metricname: "filter1_metric", Operator: "LessThan", Target: 20,
					Labels: []string{}}}},
			want: true,
		},
		{
			name:   "replace policy by policy with wrong metric rule",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy1, policy6},
			expect: nil,
			want:   false,
		},
		{
			name:   "replace policy by an empty policy",
			fields: fields{interfaceMock{}, cache.MockCache{}, strategy.MockStrategy{}},
			args:   args{policy1, policy7},
			expect: nil,
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			controller := &TelemetryPolicyController{
				Interface: tt.fields.Interface,
				Writer:    tt.fields.Writer,
				Enforcer:  &tt.fields.Enforcer,
			}
			controller.onUpdate(tt.args.old, tt.args.new)
			enforced := tt.fields.Enforcer
			gotStrMetricRule := enforced.AddedStrategies.I
			if gotStrMetricRule == nil {
				got = false
			} else {
				got = reflect.DeepEqual(tt.expect, gotStrMetricRule)
			}
			if got != tt.want {
				t.Errorf("Got %v, want %v", gotStrMetricRule, tt.expect)
			}
		})
	}
}

/*
var mockServer = httptest.Server{
	URL: "localhost:9090",
}

func TestTelemetryPolicyController_Run(t *testing.T) {
	type fields struct {
		TelemetryPolicyClient rest.Interface
		Cache                 cache.ReaderWriter
		Enforcer              strategy.Enforcer
	}

	type args struct {
		context context.Context
	}

	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// {"basic test",
		//	fields{fakeRESTClient(), metrics.NewAutoUpdatingCache(), &strategy.MetricEnforcer{}},
		//	args{context.Background()},
		// },
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			controller := &TelemetryPolicyController{
				tt.fields.TelemetryPolicyClient,
				tt.fields.Cache,
				tt.fields.Enforcer,
			}
			controller.Run(tt.args.context)
		})
		mockServer.Close()
	}
}
*/
