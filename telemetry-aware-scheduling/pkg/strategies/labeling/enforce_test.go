// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package labeling

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	strategy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	telpol "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testclient "k8s.io/client-go/kubernetes/fake"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
)

func TestLabelingStrategy_Enforce(t *testing.T) {
	type args struct {
		enforcer *strategy.MetricEnforcer
		cache    cache.ReaderWriter
	}

	type expected struct {
		nodeLabels map[string]string
		nodeNames  []string
	}

	tests := []struct {
		name    string
		d       *Strategy
		node    *v1.Node
		args    args
		wantErr bool
		want    expected
	}{ // this should test the labeling capacity on the node with metric that violates the labeling strategy rule.
		{name: "node labelled",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "GreaterThan", Target: 99, Labels: []string{"gpu-card1=false"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{"telemetry.aware.scheduling.labeling-test/gpu-card1": "false"}}},
		// this should test no label added
		{name: "node unlabeled test",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "GreaterThan", Target: 3000, Labels: []string{"gpu-card0=false"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},

		// this should test two labels added
		{name: "node labelled two different metrics",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "GreaterThan", Target: 99, Labels: []string{"gpu-card1=false"}},
					{Metricname: "cpu", Operator: "GreaterThan", Target: 10, Labels: []string{"gpu-card2=true"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{"telemetry.aware.scheduling.labeling-test/gpu-card1": "false",
				"telemetry.aware.scheduling.labeling-test/gpu-card2": "true"}}},

		// this should test single label preferred added by the minmax
		// same label key: gpu-device - metric "memory" > "cpu"
		{name: "node single labelled: -different metrics -same op, tag, and label key",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "GreaterThan", Target: 100, Labels: []string{"gpu-device=card0"}},
					{Metricname: "cpu", Operator: "GreaterThan", Target: 100, Labels: []string{"gpu-device=card1"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{"telemetry.aware.scheduling.labeling-test/gpu-device": "card0"}}},

		// this should test single label preferred added by the minmax
		// same label key: gpu-device - metric "memory" < "cpu"
		{name: "node single labelled: -different metrics -same op, tag, and label key",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "LessThan", Target: 10000, Labels: []string{"gpu-device=card0"}},
					{Metricname: "cpu", Operator: "LessThan", Target: 10000, Labels: []string{"gpu-device=card1"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{"telemetry.aware.scheduling.labeling-test/gpu-device": "card1"}}},

		{name: "node single labelled: -different metrics and tag, same op and label keys",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "Equals", Target: 2000, Labels: []string{"gpu-device=card1"}},
					{Metricname: "cpu", Operator: "Equals", Target: 200, Labels: []string{"gpu-device=card0"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{"telemetry.aware.scheduling.labeling-test/gpu-device": "card1"}}},

		{name: "node single labelled: -different metrics and tag, same op and label keys",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "Equals", Target: 2000, Labels: []string{"gpu-device=card0"}},
					{Metricname: "cpu", Operator: "Equals", Target: 200, Labels: []string{"gpu-device=card1"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{"telemetry.aware.scheduling.labeling-test/gpu-device": "card0"}}},

		{name: "node single labelled: -different metrics and tag, same op and label keys",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "Equals", Target: 2000, Labels: []string{"gpu-device=card0"}},
					{Metricname: "cpu", Operator: "Equals", Target: 200, Labels: []string{"gpu-device=card1"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{"telemetry.aware.scheduling.labeling-test/gpu-device": "card0"}}},
	}

	for _, tt := range tests {
		tt := tt

		err := tt.args.cache.WriteMetric("memory", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1,
			Value: *resource.NewQuantity(2000, resource.DecimalSI)}})
		if err != nil {
			t.Errorf("Cannot write metric to mock cach for test: %v", err)
		}

		err = tt.args.cache.WriteMetric("cpu", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1,
			Value: *resource.NewQuantity(200, resource.DecimalSI)}})
		if err != nil {
			t.Errorf("Cannot write metric to mock cach for test: %v", err)
		}

		_, err = tt.args.enforcer.KubeClient.CoreV1().Nodes().Create(context.TODO(), tt.node, metav1.CreateOptions{})
		if err != nil {
			t.Errorf("Cannot write metric to mock cach for test: %v", err)
		}

		tt.args.enforcer.RegisterStrategyType(tt.d)
		tt.args.enforcer.AddStrategy(tt.d, tt.d.StrategyType())

		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			tmp := map[string]string{}

			_, err := tt.d.Enforce(tt.args.enforcer, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("Strategy.Enforce() error = %v, wantErr %v", err, tt.wantErr)
			}
			nodys, _ := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			for _, node := range nodys.Items {
				tmp = node.Labels
				got = append(got, node.Name)
			}
			if len(got) != len(tt.want.nodeNames) {
				t.Errorf("Nodes returned: %v not as expected: %v", got, tt.want.nodeNames)
			}
			if len(tmp) != len(tt.want.nodeLabels) {
				t.Errorf("Number of labels returned: %v not as expected: %v", len(tmp), len(tt.want.nodeLabels))
			}
			if !reflect.DeepEqual(tmp, tt.want.nodeLabels) {
				t.Errorf("labels returned: %v not as expected: %v", tmp, tt.want.nodeLabels)
			}
		})
	}
}

func TestLabelingStrategy_Enforce_unsupportedCases(t *testing.T) {
	type args struct {
		enforcer *strategy.MetricEnforcer
		cache    cache.ReaderWriter
	}

	type expected struct {
		nodeLabels map[string]string
		nodeNames  []string
	}

	tests := []struct {
		name    string
		d       *Strategy
		node    *v1.Node
		args    args
		wantErr bool
		want    expected
	}{
		{name: "node not supported label: -different metrics and op, same tag and label keys",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "GreaterThan", Target: 200, Labels: []string{"gpu-device=card0"}},
					{Metricname: "cpu", Operator: "LessThan", Target: 200, Labels: []string{"gpu-device=card1"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},

		{name: "node not supported label: -different metrics, op, and tag - The same label keys",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "LessThan", Target: 2000, Labels: []string{"gpu-device=card0"}},
					{Metricname: "cpu", Operator: "Equals", Target: 200, Labels: []string{"gpu-device=card1"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},

		{name: "node not supported label: -different metrics, and tag - the same label keys",
			d: &Strategy{
				PolicyName: "labeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "GreaterThan", Target: 20, Labels: []string{"gpu-device=card1"}},
					{Metricname: "cpu", Operator: "Equals", Target: 200, Labels: []string{"gpu-device=card0"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.labeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},
	}

	for _, tt := range tests {
		tt := tt

		err := tt.args.cache.WriteMetric("memory", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1,
			Value: *resource.NewQuantity(200, resource.DecimalSI)}})
		if err != nil {
			t.Errorf("Cannot write metric to mock cache for test: %v", err)
		}

		err = tt.args.cache.WriteMetric("cpu", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1,
			Value: *resource.NewQuantity(200, resource.DecimalSI)}})
		if err != nil {
			t.Errorf("Cannot write metric to mock cache for test: %v", err)
		}

		_, err = tt.args.enforcer.KubeClient.CoreV1().Nodes().Create(context.TODO(), tt.node, metav1.CreateOptions{})
		if err != nil {
			t.Errorf("Cannot write metric to mock cache for test: %v", err)
		}

		tt.args.enforcer.RegisterStrategyType(tt.d)
		tt.args.enforcer.AddStrategy(tt.d, tt.d.StrategyType())

		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			tmp := map[string]string{}
			klog.Info(tmp)

			_, err := tt.d.Enforce(tt.args.enforcer, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("Strategy.Enforce() error = %v, wantErr %v", err, tt.wantErr)
			}
			nodys, _ := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			for _, node := range nodys.Items {
				tmp = node.Labels
				got = append(got, node.Name)
			}
			if len(got) != len(tt.want.nodeNames) {
				t.Errorf("Nodes returned: %v not as expected: %v", got, tt.want.nodeNames)
			}
			if len(tmp) != len(tt.want.nodeLabels) {
				t.Errorf("Number of labels returned: %v not as expected: %v", len(tmp), len(tt.want.nodeLabels))
			}
			if !reflect.DeepEqual(tmp, tt.want.nodeLabels) {
				t.Errorf("labels returned: %v not as expected: %v", tmp, tt.want.nodeLabels)
			}
		})
	}
}

func TestLabelingStrategy_Cleanup(t *testing.T) {
	type args struct {
		enforcer *strategy.MetricEnforcer
		cache    cache.ReaderWriter
	}

	type expected struct {
		nodeLabels map[string]string
		nodeNames  []string
	}

	tests := []struct {
		name    string
		node    *v1.Node
		d       *Strategy
		args    args
		wantErr bool
		want    expected
	}{ // this should test the labeling capacity on the node with metric that violates the labeling strategy rule.
		{name: "node labelled then unlabelled",
			d: &Strategy{
				PolicyName: "unlabeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "GreaterThan", Target: 99, Labels: []string{"gpu-card1=false"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.unlabeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},

		// this should test no label added and no label removed from the policy
		{name: "node unlabeled test",
			d: &Strategy{
				PolicyName: "unlabeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "GreaterThan", Target: 3000, Labels: []string{"gpu-card0=false"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.unlabeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},

		// this should test two labels added and both removed after cleanup
		{name: "node labelled two different metrics",
			d: &Strategy{
				PolicyName: "unlabeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "GreaterThan", Target: 99, Labels: []string{"gpu-card1=false"}},
					{Metricname: "cpu", Operator: "GreaterThan", Target: 10, Labels: []string{"gpu-card2=true"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.unlabeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},

		// this should test label removal on single label preferred added by the minmax
		{name: "node single labelled: -different metrics -same op, tag, and label key",
			d: &Strategy{
				PolicyName: "unlabeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "GreaterThan", Target: 100, Labels: []string{"gpu-device=card0"}},
					{Metricname: "cpu", Operator: "GreaterThan", Target: 100, Labels: []string{"gpu-device=card1"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.unlabeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},

		// this should test removal of the single label preferred added by the minmax
		{name: "node single labelled: -different metrics -same op, tag, and label key",
			d: &Strategy{
				PolicyName: "unlabeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "LessThan", Target: 10000, Labels: []string{"gpu-device=card0"}},
					{Metricname: "cpu", Operator: "LessThan", Target: 10000, Labels: []string{"gpu-device=card1"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.unlabeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},

		// this should test removal of the single label preferred added by the minmax
		{name: "node single labelled: -different metrics and tag, same op and label keys",
			d: &Strategy{
				PolicyName: "unlabeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "Equals", Target: 2000, Labels: []string{"gpu-device=card1"}},
					{Metricname: "cpu", Operator: "Equals", Target: 200, Labels: []string{"gpu-device=card0"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.unlabeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},

		// this should test removal of the single label preferred added by the minmax
		{name: "node single labelled: -different metrics and tag, same op and label keys",
			d: &Strategy{
				PolicyName: "unlabeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "Equals", Target: 2000, Labels: []string{"gpu-device=card0"}},
					{Metricname: "cpu", Operator: "Equals", Target: 200, Labels: []string{"gpu-device=card1"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.unlabeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},

		// this should test removal of the single label preferred added by the minmax
		{name: "node single labelled: -different metrics and tag, same op and label keys",
			d: &Strategy{
				PolicyName: "unlabeling-test",
				Rules: []telpol.TASPolicyRule{
					{Metricname: "memory", Operator: "Equals", Target: 2000, Labels: []string{"gpu-device=card0"}},
					{Metricname: "cpu", Operator: "Equals", Target: 200, Labels: []string{"gpu-device=card1"}}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"telemetry.aware.scheduling.unlabeling-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()), cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}, nodeLabels: map[string]string{}}},
	}

	for _, tt := range tests {
		tt := tt

		err := tt.args.cache.WriteMetric("memory", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1,
			Value: *resource.NewQuantity(2000, resource.DecimalSI)}})
		if err != nil {
			t.Errorf("Cannot write metric to mock cach for test: %v", err)
		}

		err = tt.args.cache.WriteMetric("cpu", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1,
			Value: *resource.NewQuantity(200, resource.DecimalSI)}})
		if err != nil {
			t.Errorf("Cannot write metric to mock cach for test: %v", err)
		}

		_, err = tt.args.enforcer.KubeClient.CoreV1().Nodes().Create(context.TODO(), tt.node, metav1.CreateOptions{})
		if err != nil {
			t.Errorf("Cannot write metric to mock cach for test: %v", err)
		}

		tt.args.enforcer.RegisterStrategyType(tt.d)
		tt.args.enforcer.AddStrategy(tt.d, tt.d.StrategyType())

		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			tmp := map[string]string{}

			_, err := tt.d.Enforce(tt.args.enforcer, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("Strategy.Enforce() error = %v, wantErr %v", err, tt.wantErr)
			}

			err = tt.d.Cleanup(tt.args.enforcer, tt.d.PolicyName) // testing Cleanup()
			if err != nil {
				klog.InfoS(err.Error(), "component", "testing")
			}
			nodys, _ := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			for _, node := range nodys.Items {
				tmp = node.Labels
				got = append(got, node.Name)
			}
			if len(got) != len(tt.want.nodeNames) {
				t.Errorf("Nodes returned: %v not as expected: %v", got, tt.want.nodeNames)
			}
			if len(tmp) != len(tt.want.nodeLabels) {
				t.Errorf("Number of labels returned: %v not as expected: %v", len(tmp), len(tt.want.nodeLabels))
			}
			if !reflect.DeepEqual(tmp, tt.want.nodeLabels) {
				t.Errorf("labels returned: %v not as expected: %v", tmp, tt.want.nodeLabels)
			}
		})
	}
}
