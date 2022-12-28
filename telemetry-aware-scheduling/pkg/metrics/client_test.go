// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"k8s.io/klog/v2"

	"k8s.io/client-go/tools/clientcmd"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	restclient "k8s.io/client-go/rest"
	core "k8s.io/client-go/testing"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	custommetricsapi "k8s.io/metrics/pkg/apis/custom_metrics/v1beta2"
	customclient "k8s.io/metrics/pkg/client/custom_metrics"
	cmfake "k8s.io/metrics/pkg/client/custom_metrics/fake"
)

var baseTimeStamp = time.Date(2019, time.May, 20, 12, 25, 00, 0, time.UTC)

// As in NewTestFactory method from kubectl/testing/fake.go
// Reproduced rather than referenced because of dependency issues.
func dummyRestClientConfig() *restclient.Config {
	defer func() {
		err := recover()
		if err != nil {
			klog.InfoS("Recovered from runtime error", "component", "testing")
		}
	}()

	tmpFile, err := os.CreateTemp("", "cmdtests_temp")
	if err != nil {
		klog.InfoS("Unable to create a fake client config", "component", "testing")
		klog.Exit(err.Error())
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{
		Precedence:     []string{tmpFile.Name()},
		MigrationRules: map[string]string{},
	}
	overrides := &clientcmd.ConfigOverrides{ClusterDefaults: clientcmdapi.Cluster{Server: "http://localhost:8080"}}
	fallbackReader := bytes.NewBuffer([]byte{})
	clientConfig := clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules, overrides, fallbackReader)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		klog.InfoS("Can't create dummy rest client config: "+err.Error(), "component", "testing")
	}

	return restConfig
}

func dummyMetric(level int64, metricName string, timestamp time.Time, nodeID int) custommetricsapi.MetricValue {
	window := int64(60)

	return custommetricsapi.MetricValue{
		DescribedObject: v1.ObjectReference{
			Kind:       "Node",
			APIVersion: "v1alpha1",
			Name:       fmt.Sprintf("%s-%d", "node", nodeID),
		},
		WindowSeconds: &window,
		Value:         *resource.NewQuantity(level, resource.DecimalSI),
		Timestamp:     metav1.Time{Time: timestamp},
		Metric: custommetricsapi.MetricIdentifier{
			Name: metricName,
		},
	}
}

func setUpFakeClient(dummyMetrics custommetricsapi.MetricValueList) *cmfake.FakeCustomMetricsClient {
	fakeCMClient := &cmfake.FakeCustomMetricsClient{}
	fakeCMClient.AddReactor("get", "nodes", func(action core.Action) (handled bool, ret runtime.Object, err error) {
		metrics := &dummyMetrics
		getForAction, _ := action.(cmfake.GetForAction)
		if getForAction.GetMetricName() != "memoryFree" {
			return true, nil, fmt.Errorf("no metric of that name found %s %w", action, errNull)
		}

		return true, metrics, nil
	})

	return fakeCMClient
}

func Test_customMetricsClient_GetNodeMetric(t *testing.T) {
	type fields struct {
		client customclient.CustomMetricsClient
	}

	type args struct {
		metricName string
	}

	dummyMetrics := custommetricsapi.MetricValueList{
		Items: []custommetricsapi.MetricValue{dummyMetric(50, "memoryFree", baseTimeStamp, 1)},
	}
	dm := setUpFakeClient(dummyMetrics)
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    NodeMetricsInfo
		wantErr bool
	}{
		{"correct metric retrieved", fields{dm}, args{"memoryFree"},
			NodeMetricsInfo{"node-1": NodeMetric{baseTimeStamp, 1 * time.Minute,
				*resource.NewQuantity(50, resource.DecimalSI)}}, false},
		{"non existent metric query", fields{dm}, args{"nonExistentMetric"},
			NodeMetricsInfo{}, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			c := CustomMetricsClient{
				tt.fields.client,
			}
			got, err := c.GetNodeMetric(tt.args.metricName)

			if (err != nil) != tt.wantErr {
				t.Errorf("customMetricsClient.GetNodeMetric() error = %v, wantErr %v", err, tt.wantErr)

				return
			}
			if err != nil && tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("customMetricsClient.GetNodeMetric() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	type args struct {
		config *restclient.Config
	}

	tests := []struct {
		name string
		args args
	}{
		{"valid config", args{dummyRestClientConfig()}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := NewClient(tt.args.config)
			if reflect.TypeOf(got) != reflect.TypeOf(dummyRestClientConfig()) {
				klog.InfoS("No real test implemented here", "component", "testing")
				//TODO:add some better verification constructor has worked here.
			}
		})
	}
}
