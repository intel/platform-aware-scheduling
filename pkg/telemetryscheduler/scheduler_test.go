//Logic for the scheduler extender - including the server it starts and prioritize + filter methods - is implemented in this package.
package telemetryscheduler

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/intel/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/telemetry-aware-scheduling/pkg/scheduler"
	telpolv1 "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	telpolclient "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/client/v1alpha1"
	"io/ioutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

var prioritizerArgs1 = scheduler.ExtenderArgs{
	Pod:       v1.Pod{TypeMeta: metav1.TypeMeta{}, ObjectMeta: metav1.ObjectMeta{Name: "big pod", Labels: map[string]string{"telemetry-policy": "test-policy"}, Namespace: "default"}},
	Nodes:     &v1.NodeList{Items: []v1.Node{{TypeMeta: metav1.TypeMeta{}, ObjectMeta: metav1.ObjectMeta{Name: "node A"}, Spec: v1.NodeSpec{}, Status: v1.NodeStatus{}}}},
	NodeNames: &[]string{"node A", "node B"},
}

var twoNodeArgument = scheduler.ExtenderArgs{
	Pod:       v1.Pod{TypeMeta: metav1.TypeMeta{}, ObjectMeta: metav1.ObjectMeta{Name: "big pod", Labels: map[string]string{"telemetry-policy": "test-policy"}, Namespace: "default"}},
	Nodes:     &v1.NodeList{Items: []v1.Node{{TypeMeta: metav1.TypeMeta{}, ObjectMeta: metav1.ObjectMeta{Name: "node A"}, Spec: v1.NodeSpec{}, Status: v1.NodeStatus{}}, {TypeMeta: metav1.TypeMeta{}, ObjectMeta: metav1.ObjectMeta{Name: "node B"}, Spec: v1.NodeSpec{}, Status: v1.NodeStatus{}}}},
	NodeNames: &[]string{"node A", "node B"},
}

var noPolicyPod = scheduler.ExtenderArgs{
	Pod:       v1.Pod{TypeMeta: metav1.TypeMeta{}, ObjectMeta: metav1.ObjectMeta{Name: "big pod", Labels: map[string]string{"useless-label": "test-policy"}, Namespace: "default"}},
	Nodes:     &v1.NodeList{Items: []v1.Node{{TypeMeta: metav1.TypeMeta{}, ObjectMeta: metav1.ObjectMeta{Name: "node A"}, Spec: v1.NodeSpec{}, Status: v1.NodeStatus{}}}},
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
					{Metricname:"dummyMetric1", Operator: "GreaterThan", Target: 40},
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
		prioritizeArgs scheduler.ExtenderArgs
		wanted         scheduler.HostPriorityList
		wantErr        bool
	}{
		{name: "unlabelled pod",
			fields: fields{*dummyClient, cache.MockSelfUpdatingCache(),
				testPolicy1},
			args:           args{httptest.NewRequest("POST", "http://localhost/scheduler/prioritize", nil)},
			metric:         map[string]metrics.NodeMetric{"node A": {Value: *resource.NewQuantity(100, resource.DecimalSI)}, "node B": {Value: *resource.NewQuantity(90, resource.DecimalSI)}},
			prioritizeArgs: noPolicyPod,
			wanted:         []scheduler.HostPriority{{Host: "node A", Score: 10}, {Host: "node B", Score: 9}},
			wantErr:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMetricsExtender(tt.fields.cache)
			err := tt.fields.cache.WritePolicy(tt.fields.policy.Namespace, tt.fields.policy.Name, tt.fields.policy)
			if err != nil && tt.wantErr {
				log.Print(err)
				return
			}
			argsAsJSON, err := json.Marshal(tt.prioritizeArgs)
			if err != nil && tt.wantErr {
				log.Print(err)
				return
			}
			err = tt.fields.cache.WriteMetric(tt.fields.policy.Spec.Strategies["scheduleonmetric"].Rules[0].Metricname, tt.metric)
			if err != nil && tt.wantErr {
				return
			}
			tt.args.r.Header.Add("Content-Type", "application/json")
			tt.args.r.Body = ioutil.NopCloser(bytes.NewReader(argsAsJSON))
			w := httptest.NewRecorder()
			m.Prioritize(w, tt.args.r)
			result := scheduler.HostPriorityList{}
			b := w.Body.Bytes()
			err = json.Unmarshal(b, &result)
			log.Print(result)
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
		prioritizeArgs scheduler.ExtenderArgs
		wanted         scheduler.HostPriorityList
		wantErr        bool
	}{
		{"get and return node test",
			fields{*dummyClient, cache.MockSelfUpdatingCache(),
				testPolicy1},
			args{httptest.NewRequest("POST", "http://localhost/scheduler/prioritize", nil)},
			map[string]metrics.NodeMetric{"node A": {Value: *resource.NewQuantity(100, resource.DecimalSI)}, "node B": {Value: *resource.NewQuantity(90, resource.DecimalSI)}},
			twoNodeArgument,
			[]scheduler.HostPriority{{Host: "node A", Score: 10}, {Host: "node B", Score: 9}},
			false,
		},
		{"policy not found",
			fields{*dummyClient, cache.MockSelfUpdatingCache(),
				testPolicy2},
			args{httptest.NewRequest("POST", "http://localhost/scheduler/prioritize", nil)},
			map[string]metrics.NodeMetric{"node A": {Value: *resource.NewQuantity(90, resource.DecimalSI)}, "node B": {Value: *resource.NewQuantity(100, resource.DecimalSI)}},
			twoNodeArgument,
			[]scheduler.HostPriority{},
			true,
		},
		{"cache returns error if empty",
			fields{*dummyClient, cache.MockEmptySelfUpdatingCache(),
				testPolicy1},
			args{httptest.NewRequest("POST", "http://localhost/scheduler/prioritize", nil)},
			map[string]metrics.NodeMetric{"node A": {Value: *resource.NewQuantity(100, resource.DecimalSI)}},
			prioritizerArgs1,
			[]scheduler.HostPriority{{Host: "node B", Score: 10}},
			true,
		},
		{"malformed arguments return error",fields{*dummyClient, cache.MockEmptySelfUpdatingCache(),
				testPolicy1},
			args{httptest.NewRequest("POST", "http://localhost/scheduler/prioritize", nil)},
			map[string]metrics.NodeMetric{"node A": {Value: *resource.NewQuantity(100, resource.DecimalSI)}},
			scheduler.ExtenderArgs{},
			[]scheduler.HostPriority{{Host: "node B", Score: 10}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMetricsExtender(tt.fields.cache)
			err := tt.fields.cache.WritePolicy(tt.fields.policy.Namespace, tt.fields.policy.Name, tt.fields.policy)
			if err != nil && tt.wantErr {
				log.Print(err)
				return
			}
			argsAsJSON, err := json.Marshal(tt.prioritizeArgs)
			if err != nil && tt.wantErr {
				log.Print(err)
				return
			}
			err = tt.fields.cache.WriteMetric(tt.fields.policy.Spec.Strategies["scheduleonmetric"].Rules[0].Metricname, tt.metric)
			if err != nil && tt.wantErr {
				log.Print(err)
				return
			}
			tt.args.r.Header.Add("Content-Type", "application/json")
			tt.args.r.Body = ioutil.NopCloser(bytes.NewReader(argsAsJSON))
			w := httptest.NewRecorder()
			m.Prioritize(w, tt.args.r)
			result := scheduler.HostPriorityList{}
			b := w.Body.Bytes()
			err = json.Unmarshal(b, &result)
			if err != nil && tt.wantErr {
				log.Print(err)
				return
			}
			if len(result) == 0 {
				if !tt.wantErr {
					t.Errorf("No nodes returned ")
				}
			}
			if len(result) == len(tt.wanted) {
				log.Print(result, tt.wanted)
				for i, priorityItem := range result {
					if priorityItem.Host != tt.wanted[i].Host {
						err = errors.New("host names not equal")
					}
					if priorityItem.Score != tt.wanted[i].Score {
						err = errors.New("scores not equal")
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
		wanted  scheduler.ExtenderFilterResult
		wantErr bool
	}{
		{name: "get and return node test",
			fields: fields{*dummyClient,
				cache.MockSelfUpdatingCache(),
				metrics.NewDummyMetricsClient(metrics.InstanceOfMockMetricClientMap),
				testPolicy1},
			args: args{
				httptest.NewRequest("POST", "http://localhost/scheduler/prioritize", nil), metrics.TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{10, 30})},
			wanted: scheduler.ExtenderFilterResult{Nodes: &v1.NodeList{}, NodeNames: &[]string{"node A"}, FailedNodes: map[string]string{}},
		},
		{name: "filter out one node",
			fields: fields{*dummyClient, cache.MockSelfUpdatingCache(),
				metrics.NewDummyMetricsClient(metrics.InstanceOfMockMetricClientMap),
				testPolicy1},
			args:   args{httptest.NewRequest("POST", "http://localhost/scheduler/prioritize", nil), metrics.TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{50, 30})},
			wanted: scheduler.ExtenderFilterResult{Nodes: &v1.NodeList{}, NodeNames: &[]string{"node A"}, FailedNodes: map[string]string{"node A": ""}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := MetricsExtender{
				cache: tt.fields.cache,
			}
			err := tt.fields.cache.WritePolicy(tt.fields.policy.Namespace, tt.fields.policy.Name, tt.fields.policy)
			if err != nil && tt.wantErr {
				log.Print(err)
				return
			}
			err = tt.fields.cache.WriteMetric(tt.fields.policy.Spec.Strategies["dontschedule"].Rules[0].Metricname, tt.args.metric)
			if err != nil && tt.wantErr {
				log.Print(err)
				return
			}
			argsAsJSON, err := json.Marshal(twoNodeArgument)
			if err != nil {
				log.Print(err)
			}
			tt.args.r.Body = ioutil.NopCloser(bytes.NewReader(argsAsJSON))
			tt.args.r.Header.Add("Content-Type", "application/json")
			w := httptest.NewRecorder()
			m.Filter(w, tt.args.r)
			result := scheduler.ExtenderFilterResult{}
			b := w.Body.Bytes()
			err = json.Unmarshal(b, &result)
			if err != nil {
				t.Errorf("problem unmarshalling response %v", err)
				return
			}
			log.Print(result)
			if len(result.FailedNodes) == len(tt.wanted.FailedNodes) {
				for name := range result.FailedNodes {
					if _, ok := tt.wanted.FailedNodes[name]; !ok {
						err = errors.New("host names not found " + name)
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

