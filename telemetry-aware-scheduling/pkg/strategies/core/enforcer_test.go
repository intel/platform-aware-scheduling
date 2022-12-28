// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"reflect"
	"testing"

	"k8s.io/klog/v2"

	"k8s.io/client-go/kubernetes"
	testclient "k8s.io/client-go/kubernetes/fake"
)

var mockedStrategy = &MockStrategy{StrategyTypeMock: "mocko"}

func TestNewEnforcer(t *testing.T) {
	type args struct {
		kubeClient kubernetes.Interface
	}

	tests := []struct {
		name string
		args args
		want *MetricEnforcer
	}{
		// TODO: add test cases.
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := NewEnforcer(tt.args.kubeClient); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewEnforcer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetricEnforcer_RegisterStrategyType(t *testing.T) {
	type fields struct {
		RegisteredTypes   map[string]map[Interface]interface{}
		PodViolatingLabel string
		KubeClient        kubernetes.Interface
	}

	type args struct {
		str Interface
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		wanted []string
	}{
		{"RegisterStrategyType one strategy",
			fields{make(map[string]map[Interface]interface{}),
				"violates",
				testclient.NewSimpleClientset()},
			args{str: mockedStrategy},
			[]string{"mocko"}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			e := &MetricEnforcer{
				RegisteredStrategies: tt.fields.RegisteredTypes,
				KubeClient:           tt.fields.KubeClient,
			}
			e.RegisterStrategyType(tt.args.str)
			if len(e.RegisteredStrategyTypes()) != len(tt.wanted) {
				t.Error("Number of items in registry wrong")
			}
			for i, v := range e.RegisteredStrategyTypes() {
				if v != tt.wanted[i] {
					t.Error("Registered Types not as expected")
				}
			}
		})
	}
}

func TestMetricEnforcer_UnregisterStrategyType(t *testing.T) {
	type fields struct {
		RegisteredTypes   map[string]map[Interface]interface{}
		PodViolatingLabel string
		KubeClient        kubernetes.Interface
	}

	type args struct {
		str Interface
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		wanted []string
	}{
		{"RegisterStrategyType working",
			fields{make(map[string]map[Interface]interface{}),
				"violates",
				testclient.NewSimpleClientset()},
			args{str: mockedStrategy},
			[]string{}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			e := &MetricEnforcer{
				RegisteredStrategies: tt.fields.RegisteredTypes,
				KubeClient:           tt.fields.KubeClient,
			}
			e.RegisterStrategyType(tt.args.str)
			e.UnregisterStrategyType(tt.args.str)
			if len(e.RegisteredStrategyTypes()) != len(tt.wanted) {
				t.Error("Number of items in registry wrong")
			}
			for i, v := range e.RegisteredStrategyTypes() {
				if v != tt.wanted[i] {
					t.Error("Registered Types not as expected")
				}
			}
		})
	}
}

func TestMetricEnforcer_RegisteredStrategyTypes(t *testing.T) {
	type fields struct {
		RegisteredTypes   map[string]map[Interface]interface{}
		PodViolatingLabel string
		KubeClient        kubernetes.Interface
	}

	tests := []struct {
		name   string
		fields fields
		want   []string
	}{
		{"single strategy",
			fields{RegisteredTypes: map[string]map[Interface]interface{}{"toughStrategy": nil},
				PodViolatingLabel: "",
				KubeClient:        testclient.NewSimpleClientset()},
			[]string{"toughStrategy"}},
		{"no strategies",
			fields{RegisteredTypes: map[string]map[Interface]interface{}{},
				PodViolatingLabel: "",
				KubeClient:        testclient.NewSimpleClientset()},
			[]string{}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			e := &MetricEnforcer{
				RegisteredStrategies: tt.fields.RegisteredTypes,
				KubeClient:           tt.fields.KubeClient,
			}
			if got := e.RegisteredStrategyTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MetricEnforcer.RegisteredStrategyTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetricEnforcer_AddStrategy(t *testing.T) {
	type fields struct {
		RegisteredStrategies map[string]map[Interface]interface{}
		KubeClient           kubernetes.Interface
	}

	type args struct {
		str          Interface
		strategyType string
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		wanted map[string]Interface
	}{
		{"add single strategy",
			fields{RegisteredStrategies: make(map[string]map[Interface]interface{}),
				KubeClient: testclient.NewSimpleClientset()},
			args{str: mockedStrategy, strategyType: "mocko"}, map[string]Interface{"mocko:": mockedStrategy}},
		{"duplicate added",
			fields{RegisteredStrategies: map[string]map[Interface]interface{}{"mocko": {mockedStrategy: nil}},
				KubeClient: testclient.NewSimpleClientset()},
			args{str: mockedStrategy, strategyType: "mocko"}, map[string]Interface{"mocko": mockedStrategy}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			e := &MetricEnforcer{
				RegisteredStrategies: tt.fields.RegisteredStrategies,
				KubeClient:           tt.fields.KubeClient,
			}
			e.RegisterStrategyType(tt.args.str)
			e.AddStrategy(tt.args.str, tt.args.strategyType)
			for str := range e.RegisteredStrategies[tt.args.strategyType] {
				klog.InfoS("Test metric enforcer", "registered strategies",
					e.RegisteredStrategies[tt.args.strategyType], "component", "controller")
				sart := tt.wanted[str.StrategyType()]
				if lsart, ok := sart.(*MockStrategy); ok {
					if !str.Equals(lsart) {
						t.Error("Strategies do not match")
					}
				}
			}
		})
	}
}

// func TestMetricEnforcer_EnforceRegisteredStrategies(t *testing.T) {
//	type fields struct {
//		RegisteredStrategies   map[string]map[Interface]interface{}
//		KubeClient        kubernetes.Interface
//	}
//	type args struct {
//		cache cache.ReaderWriter
//		timer time.Ticker
//	}
//	tests := []struct {
//		name   string
//		fields fields
//		args   args
//	}{
//		{"two strategies Enforced",
//			fields{map[string]map[Interface]interface{}{"mocko": {mockedStrategy:nil}},
//				testclient.NewSimpleClientset()},
//				args{dummyCache, *time.NewTicker(1 * time.Millisecond)}},
//	}
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			limit := time.NewTicker(1 * time.Second)
//			e := &MetricEnforcer{
//				RegisteredStrategies: tt.fields.RegisteredStrategies,
//				KubeClient:           tt.fields.KubeClient,
//			}
//			go e.EnforceRegisteredStrategies(tt.args.cache, tt.args.timer)
//			<-limit.C
//			testclient.Clientset{}
//			return
//		TODO: finda good way to test this method
//		})
//	}
//}

func TestMetricEnforcer_IsRegistered(t *testing.T) {
	type fields struct {
		RegisteredStrategies map[string]map[Interface]interface{}
		KubeClient           kubernetes.Interface
	}

	type args struct {
		str string
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{"strategy is registered",
			fields{map[string]map[Interface]interface{}{"mocko": {mockedStrategy: nil}, "socko": {}},
				testclient.NewSimpleClientset()},
			args{"mocko"}, true},
		{"strategy not registered",
			fields{map[string]map[Interface]interface{}{"mocko": {mockedStrategy: nil}, "socko": {}},
				testclient.NewSimpleClientset()},
			args{"not registered"}, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			e := &MetricEnforcer{
				RegisteredStrategies: tt.fields.RegisteredStrategies,
				KubeClient:           tt.fields.KubeClient,
			}
			if got := e.IsRegistered(tt.args.str); got != tt.want {
				t.Errorf("MetricEnforcer.IsRegistered() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetricEnforcer_RemoveStrategy(t *testing.T) {
	type fields struct {
		RegisteredStrategies map[string]map[Interface]interface{}
		KubeClient           kubernetes.Interface
	}

	type args struct {
		str          Interface
		strategyType string
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{"strategy removed",
			fields{map[string]map[Interface]interface{}{"mocko": {mockedStrategy: nil}, "socko": {}},
				testclient.NewSimpleClientset()},
			args{mockedStrategy, mockedStrategy.StrategyType()}, false},
		{"wrong type, stategy not removed",
			fields{map[string]map[Interface]interface{}{"mocko": {mockedStrategy: nil}, "socko": {}},
				testclient.NewSimpleClientset()},
			args{mockedStrategy, "wrong type"},
			true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			e := &MetricEnforcer{
				RegisteredStrategies: tt.fields.RegisteredStrategies,
				KubeClient:           tt.fields.KubeClient,
			}
			e.RemoveStrategy(tt.args.str, tt.args.strategyType)
			for str := range e.RegisteredStrategies[tt.args.strategyType] {
				if str.StrategyType() == tt.args.strategyType {
					if tt.want != false {
						t.Error("strategy removal didn't work.")
					}
				}
			}
		})
	}
}
