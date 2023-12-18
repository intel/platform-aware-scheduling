// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Tests for the scheduler extender - including the server it starts and prioritize + filter methods - is implemented in this package.
package telemetryscheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/klog/v2"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	telpolv1 "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	telpolclient "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/client/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	extenderV1 "k8s.io/kube-scheduler/extender/v1"
)

var prioritizerArgs1 = extenderV1.ExtenderArgs{
	Pod: &v1.Pod{TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: "big pod", Labels: map[string]string{"telemetry-policy": "test-policy"}, Namespace: "default"}},
	Nodes: &v1.NodeList{Items: []v1.Node{{TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: "node A"}, Spec: v1.NodeSpec{}, Status: v1.NodeStatus{}}}},
	NodeNames: &[]string{"node A", "node B"},
}

var twoNodeArgument = extenderV1.ExtenderArgs{
	Pod: &v1.Pod{TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: "big pod", Labels: map[string]string{"telemetry-policy": "test-policy"}, Namespace: "default"}},
	Nodes: &v1.NodeList{Items: []v1.Node{{TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: "node A"}, Spec: v1.NodeSpec{}, Status: v1.NodeStatus{}}, {TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: "node B"}, Spec: v1.NodeSpec{}, Status: v1.NodeStatus{}}}},
	NodeNames: &[]string{"node A", "node B"},
}

var noPolicyPod = extenderV1.ExtenderArgs{
	Pod: &v1.Pod{TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: "big pod", Labels: map[string]string{"useless-label": "test-policy"}, Namespace: "default"}},
	Nodes: &v1.NodeList{Items: []v1.Node{{TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: "node A"}, Spec: v1.NodeSpec{}, Status: v1.NodeStatus{}}}},
	NodeNames: &[]string{"node A", "node B"},
}
var testPolicy1 = telpolv1.TASPolicy{
	TypeMeta:   metav1.TypeMeta{},
	ObjectMeta: metav1.ObjectMeta{Name: "test-policy", Namespace: "default"},
	Spec: telpolv1.TASPolicySpec{
		Strategies: map[string]telpolv1.TASPolicyStrategy{
			"scheduleonmetric": {
				PolicyName: "test-policy",
				Rules: []telpolv1.TASPolicyRule{
					{Metricname: "dummyMetric1", Operator: "GreaterThan", Target: 0}},
			},
			"dontschedule": {
				PolicyName: "test-policy",
				Rules: []telpolv1.TASPolicyRule{
					{Metricname: "dummyMetric1", Operator: "GreaterThan", Target: 40},
				},
			},
		},
	},
	Status: telpolv1.TASPolicyStatus{},
}
var testPolicy2 = telpolv1.TASPolicy{
	TypeMeta:   metav1.TypeMeta{},
	ObjectMeta: metav1.ObjectMeta{Name: "other-policy", Namespace: "default"},
	Spec: telpolv1.TASPolicySpec{
		Strategies: map[string]telpolv1.TASPolicyStrategy{
			"scheduleonmetric": {
				PolicyName: "test-policy",
				Rules: []telpolv1.TASPolicyRule{
					{Metricname: "dummyMetric1", Operator: "GreaterThan", Target: 0}},
			},
			"dontschedule": {
				PolicyName: "test-policy",
				Rules: []telpolv1.TASPolicyRule{
					{Metricname: "dummyMetric1", Operator: "GreaterThan", Target: 40},
				},
			},
		},
	},
	Status: telpolv1.TASPolicyStatus{},
}

func TestMetricsExtender_prescheduleChecks(t *testing.T) {
	dummyClient, _, _ := telpolclient.NewRest(*metrics.DummyRestClientConfig())

	type fields struct {
		telemetryPolicyClient rest.RESTClient
		cache                 cache.ReaderWriter
		policy                telpolv1.TASPolicy
	}

	type args struct {
		r *http.Request
	}

	tests := []struct {
		name           string
		fields         fields
		args           args
		metric         metrics.NodeMetricsInfo
		prioritizeArgs extenderV1.ExtenderArgs
		wanted         extenderV1.HostPriorityList
		wantErr        bool
	}{
		{name: "unlabelled pod",
			fields: fields{*dummyClient, cache.MockSelfUpdatingCache(),
				testPolicy1},
			args: args{httptest.NewRequest(http.MethodPost, "http://localhost/scheduler/prioritize", nil)},
			metric: map[string]metrics.NodeMetric{
				"node A": {Value: *resource.NewQuantity(100, resource.DecimalSI)},
				"node B": {Value: *resource.NewQuantity(90, resource.DecimalSI)}},
			prioritizeArgs: noPolicyPod,
			wanted:         []extenderV1.HostPriority{{Host: "node A", Score: 10}, {Host: "node B", Score: 9}},
			wantErr:        true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			m := NewMetricsExtender(tt.fields.cache)
			err := tt.fields.cache.WritePolicy(tt.fields.policy.Namespace, tt.fields.policy.Name, tt.fields.policy)
			if err != nil && tt.wantErr {
				klog.InfoS(err.Error(), "component", "testing")

				return
			}
			argsAsJSON, err := json.Marshal(tt.prioritizeArgs)
			if err != nil && tt.wantErr {
				klog.InfoS(err.Error(), "component", "testing")

				return
			}
			err = tt.fields.cache.WriteMetric(tt.fields.policy.Spec.Strategies["scheduleonmetric"].Rules[0].Metricname, tt.metric)
			if err != nil && tt.wantErr {
				return
			}
			tt.args.r.Header.Add("Content-Type", "application/json")
			tt.args.r.Body = io.NopCloser(bytes.NewReader(argsAsJSON))
			w := httptest.NewRecorder()
			m.Prioritize(w, tt.args.r)
			result := extenderV1.HostPriorityList{}
			b := w.Body.Bytes()
			err = json.Unmarshal(b, &result)
			msg := fmt.Sprint(result)
			klog.InfoS(msg, "component", "testing")
			if err != nil && tt.wantErr {
				return
			}
		})
	}
}

func TestMetricsExtender_Prioritize(t *testing.T) {
	dummyClient, _, _ := telpolclient.NewRest(*metrics.DummyRestClientConfig())

	type fields struct {
		telemetryPolicyClient rest.RESTClient
		cache                 cache.ReaderWriter
		policy                telpolv1.TASPolicy
	}

	type args struct {
		r *http.Request
	}

	tests := []struct {
		name           string
		fields         fields
		args           args
		metric         metrics.NodeMetricsInfo
		prioritizeArgs extenderV1.ExtenderArgs
		wanted         extenderV1.HostPriorityList
		wantErr        bool
	}{
		{"get and return node test",
			fields{*dummyClient, cache.MockSelfUpdatingCache(),
				testPolicy1},
			args{httptest.NewRequest(http.MethodPost, "http://localhost/scheduler/prioritize", nil)},
			map[string]metrics.NodeMetric{
				"node A": {Value: *resource.NewQuantity(100, resource.DecimalSI)},
				"node B": {Value: *resource.NewQuantity(90, resource.DecimalSI)}},
			twoNodeArgument,
			[]extenderV1.HostPriority{{Host: "node A", Score: 10}, {Host: "node B", Score: 9}},
			false,
		},
		{"policy not found",
			fields{*dummyClient, cache.MockSelfUpdatingCache(),
				testPolicy2},
			args{httptest.NewRequest(http.MethodPost, "http://localhost/scheduler/prioritize", nil)},
			map[string]metrics.NodeMetric{
				"node A": {Value: *resource.NewQuantity(90, resource.DecimalSI)},
				"node B": {Value: *resource.NewQuantity(100, resource.DecimalSI)}},
			twoNodeArgument,
			[]extenderV1.HostPriority{},
			true,
		},
		{"cache returns error if empty",
			fields{*dummyClient, cache.MockEmptySelfUpdatingCache(),
				testPolicy1},
			args{httptest.NewRequest(http.MethodPost, "http://localhost/scheduler/prioritize", nil)},
			map[string]metrics.NodeMetric{"node A": {Value: *resource.NewQuantity(100, resource.DecimalSI)}},
			prioritizerArgs1,
			[]extenderV1.HostPriority{{Host: "node B", Score: 10}},
			true,
		},
		{"malformed arguments return error", fields{*dummyClient, cache.MockEmptySelfUpdatingCache(),
			testPolicy1},
			args{httptest.NewRequest(http.MethodPost, "http://localhost/scheduler/prioritize", nil)},
			map[string]metrics.NodeMetric{"node A": {Value: *resource.NewQuantity(100, resource.DecimalSI)}},
			extenderV1.ExtenderArgs{},
			[]extenderV1.HostPriority{{Host: "node B", Score: 10}},
			true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			m := NewMetricsExtender(tt.fields.cache)
			err := tt.fields.cache.WritePolicy(tt.fields.policy.Namespace, tt.fields.policy.Name, tt.fields.policy)
			if err != nil && tt.wantErr {
				klog.InfoS(err.Error(), "component", "testing")

				return
			}
			argsAsJSON, err := json.Marshal(tt.prioritizeArgs)
			if err != nil && tt.wantErr {
				klog.InfoS(err.Error(), "component", "testing")

				return
			}
			err = tt.fields.cache.WriteMetric(tt.fields.policy.Spec.Strategies["scheduleonmetric"].Rules[0].Metricname, tt.metric)
			if err != nil && tt.wantErr {
				klog.InfoS(err.Error(), "component", "testing")

				return
			}
			tt.args.r.Header.Add("Content-Type", "application/json")
			tt.args.r.Body = io.NopCloser(bytes.NewReader(argsAsJSON))
			w := httptest.NewRecorder()
			m.Prioritize(w, tt.args.r)
			result := extenderV1.HostPriorityList{}
			b := w.Body.Bytes()
			err = json.Unmarshal(b, &result)
			if err != nil && tt.wantErr {
				klog.InfoS(err.Error(), "component", "testing")

				return
			}
			if len(result) == 0 {
				if !tt.wantErr {
					t.Errorf("No nodes returned ")
				}
			}
			if len(result) == len(tt.wanted) {
				msg := fmt.Sprint(result, tt.wanted)
				klog.V(2).InfoS(msg, "component", "testing")
				for i, priorityItem := range result {
					if priorityItem.Host != tt.wanted[i].Host {
						err = fmt.Errorf("host names not equal %w", errNull)
					}
					if priorityItem.Score != tt.wanted[i].Score {
						err = fmt.Errorf("scores not equal %w", errNull)
					}
				}
				if err != nil && !tt.wantErr {
					t.Errorf("error encountered %v", err)
				}
			} else {
				t.Errorf("Result list %v: different from wanted list %v:", result, tt.wanted)
			}
		})
	}
}

func TestMetricsExtender_Filter(t *testing.T) {
	dummyClient, _ := telpolclient.New(*metrics.DummyRestClientConfig(), "default")

	type fields struct {
		telemetryPolicyClient telpolclient.Client
		cache                 cache.ReaderWriter
		metricsClient         metrics.Client
		policy                telpolv1.TASPolicy
	}

	type args struct {
		r      *http.Request
		metric metrics.NodeMetricsInfo
	}

	tests := []struct {
		name    string
		fields  fields
		args    args
		wanted  extenderV1.ExtenderFilterResult
		wantErr bool
	}{
		{name: "get and return node test",
			fields: fields{*dummyClient,
				cache.MockSelfUpdatingCache(),
				metrics.NewDummyMetricsClient(metrics.InstanceOfMockMetricClientMap),
				testPolicy1},
			args: args{
				httptest.NewRequest(http.MethodPost, "http://localhost/scheduler/prioritize", nil),
				metrics.TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{10, 30})},
			wanted: extenderV1.ExtenderFilterResult{Nodes: &v1.NodeList{}, NodeNames: &[]string{"node A"}, FailedNodes: map[string]string{}},
		},
		{name: "filter out one node",
			fields: fields{*dummyClient, cache.MockSelfUpdatingCache(),
				metrics.NewDummyMetricsClient(metrics.InstanceOfMockMetricClientMap),
				testPolicy1},
			args: args{
				httptest.NewRequest(http.MethodPost, "http://localhost/scheduler/prioritize", nil),
				metrics.TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{50, 30})},
			wanted: extenderV1.ExtenderFilterResult{Nodes: &v1.NodeList{}, NodeNames: &[]string{"node A"}, FailedNodes: map[string]string{"node A": ""}},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			m := MetricsExtender{
				cache: tt.fields.cache,
			}
			err := tt.fields.cache.WritePolicy(tt.fields.policy.Namespace, tt.fields.policy.Name, tt.fields.policy)
			if err != nil && tt.wantErr {
				klog.InfoS(err.Error(), "component", "testing")

				return
			}
			err = tt.fields.cache.WriteMetric(tt.fields.policy.Spec.Strategies["dontschedule"].Rules[0].Metricname, tt.args.metric)
			if err != nil && tt.wantErr {
				klog.InfoS(err.Error(), "component", "testing")

				return
			}
			argsAsJSON, err := json.Marshal(twoNodeArgument)
			if err != nil {
				klog.InfoS(err.Error(), "component", "testing")
			}
			tt.args.r.Body = io.NopCloser(bytes.NewReader(argsAsJSON))
			tt.args.r.Header.Add("Content-Type", "application/json")
			w := httptest.NewRecorder()
			m.Filter(w, tt.args.r)
			result := extenderV1.ExtenderFilterResult{}
			b := w.Body.Bytes()
			err = json.Unmarshal(b, &result)
			if err != nil {
				t.Errorf("problem unmarshalling response %v", err)

				return
			}
			msg := fmt.Sprint(result)
			klog.InfoS(msg, "component", "testing")
			if len(result.FailedNodes) == len(tt.wanted.FailedNodes) {
				for name := range result.FailedNodes {
					if _, ok := tt.wanted.FailedNodes[name]; !ok {
						err = fmt.Errorf("host names %v not found %w", name, errNull)
					}
				}
				if err != nil && tt.wantErr == false {
					t.Errorf("error encountered %v", err)
				}
			} else {
				t.Errorf("length of returned nodes different from expected")
			}
		})
	}
}
