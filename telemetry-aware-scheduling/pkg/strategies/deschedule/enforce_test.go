// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package deschedule

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/typed/core/v1/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	strategy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	telpol "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testclient "k8s.io/client-go/kubernetes/fake"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var errMockTest = errors.New("error when calling list")

type expected struct {
	nodes        map[string]map[string]string // node name: string -> labels: map[string]string
	labeledNodes map[string]map[string]string // node name: string -> labels: map[string]string
}

type CacheMetric struct {
	metricName  string
	metricValue int64
}

func assertViolatingNodes(t *testing.T, nodeList *v1.NodeList, wantNodes map[string]map[string]string) {
	t.Helper()

	nodes := nodeList.Items
	// check lengths are equal
	if len(nodes) != len(wantNodes) {
		t.Errorf("Number of violating nodes returned: %v not as expected: %v", len(nodes), len(wantNodes))
	}

	// check if the nodes are similar
	for _, node := range nodes {
		currentNodeName := node.Name
		currentNodeLabels := node.Labels

		if wantNodes[currentNodeName] == nil {
			t.Errorf("Expected to find node %s in list of expected nodes, but wasn't there.", currentNodeName)
		}

		expectedNodeLabels := wantNodes[node.Name]
		if !reflect.DeepEqual(expectedNodeLabels, currentNodeLabels) {
			t.Errorf("Labels for node were different, expected %v got: %v", expectedNodeLabels, currentNodeLabels)
		}
	}
}

func getClientWithListException() *testclient.Clientset {
	clientWithListNodeException := testclient.NewSimpleClientset()
	clientWithListNodeException.CoreV1().(*fake.FakeCoreV1).PrependReactor("list", "nodes",
		func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, &v1.NodeList{}, errMockTest
		})

	return clientWithListNodeException
}

func getClientWithPatchException() *testclient.Clientset {
	clientWithPatchException := testclient.NewSimpleClientset()
	clientWithPatchException.CoreV1().(*fake.FakeCoreV1).PrependReactor("patch", "nodes",
		func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, errMockTest
		})

	return clientWithPatchException
}

func TestDescheduleStrategy_Enforce(t *testing.T) {
	type args struct {
		enforcer *strategy.MetricEnforcer
		cache    cache.ReaderWriter
	}

	tests := []struct {
		name                string
		d                   *Strategy
		nodes               []*v1.Node
		args                args
		wantErr             bool
		want                expected
		cacheMetrics        map[string]CacheMetric // node name: string -> metric : { metricName, metricValue }
		wantErrMessageToken string
	}{
		{name: "node label test",
			d: &Strategy{PolicyName: "deschedule-test", Rules: []telpol.TASPolicyRule{
				{Metricname: "memory", Operator: "GreaterThan", Target: 1},
				{Metricname: "cpu", Operator: "LessThan", Target: 10}}},
			nodes: []*v1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"deschedule-test": "", "node-1-label": "test"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"deschedule-test": "violating", "node-2-label": "test"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-3", Labels: map[string]string{"node-3-label": "test"}}}},
			cacheMetrics: map[string]CacheMetric{"node-2": {"cpu", 5}, "node-3": {"memory", 100}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()),
				cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{
				nodes: map[string]map[string]string{"node-1": {"node-1-label": "test"},
					"node-2": {"deschedule-test": "violating", "node-2-label": "test"},
					"node-3": {"deschedule-test": "violating", "node-3-label": "test"}},
				labeledNodes: map[string]map[string]string{"node-2": {"deschedule-test": "violating", "node-2-label": "test"},
					"node-3": {"deschedule-test": "violating", "node-3-label": "test"}},
			}},
		{name: "node unlabel test",
			d: &Strategy{PolicyName: "deschedule-test", Rules: []telpol.TASPolicyRule{
				{Metricname: "memory", Operator: "GreaterThan", Target: 1000},
				{Metricname: "cpu", Operator: "LessThan", Target: 10}}},
			nodes: []*v1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"deschedule-test": "violating", "node-1-label": "test"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"deschedule-test": "violating", "node-2-label": "test"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-3", Labels: map[string]string{"node-3-label": "test"}}}},
			cacheMetrics: map[string]CacheMetric{"node-2": {"cpu", 11}, "node-3": {"memory", 100}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()),
				cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{
				nodes: map[string]map[string]string{"node-1": {"node-1-label": "test"},
					"node-2": {"node-2-label": "test"},
					"node-3": {"node-3-label": "test"}},
				labeledNodes: map[string]map[string]string{}}},
		{name: "list nodes with exception",
			d: &Strategy{PolicyName: "deschedule-test", Rules: []telpol.TASPolicyRule{
				{Metricname: "memory", Operator: "GreaterThan", Target: 1000},
				{Metricname: "cpu", Operator: "LessThan", Target: 10}}},
			nodes: []*v1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"deschedule-test": "violating", "node-1-label": "test"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"deschedule-test": "violating", "node-2-label": "test"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-3", Labels: map[string]string{"node-3-label": "test"}}}},
			cacheMetrics: map[string]CacheMetric{"node-2": {"cpu", 11}, "node-3": {"memory", 100}},
			args: args{enforcer: strategy.NewEnforcer(getClientWithListException()),
				cache: cache.MockEmptySelfUpdatingCache()},
			want:                expected{},
			wantErr:             true,
			wantErrMessageToken: failNodeListEnforceMessage},
		{name: "list nodes with patch exception",
			d: &Strategy{PolicyName: "deschedule-test", Rules: []telpol.TASPolicyRule{
				{Metricname: "memory", Operator: "GreaterThan", Target: 1000},
				{Metricname: "cpu", Operator: "LessThan", Target: 10}}},
			nodes:        []*v1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"deschedule-test": "violating", "node-1-label": "test"}}}},
			cacheMetrics: map[string]CacheMetric{"node-1": {"cpu", 40}},
			args: args{enforcer: strategy.NewEnforcer(getClientWithPatchException()),
				cache: cache.MockEmptySelfUpdatingCache()},
			want:                expected{},
			wantErr:             true,
			wantErrMessageToken: failedLabelingMessage},
	}
	for _, tt := range tests {
		tt := tt

		for metricNodeName, metric := range tt.cacheMetrics {
			err := tt.args.cache.WriteMetric(metric.metricName, metrics.NodeMetricsInfo{
				metricNodeName: {Timestamp: time.Now(), Window: 1, Value: *resource.NewQuantity(metric.metricValue, resource.DecimalSI)}})
			if err != nil {
				t.Errorf("Cannot write metric %s to mock cache for test: %v", metricNodeName, err)
			}
		}

		// create nodes
		for _, node := range tt.nodes {
			_, err := tt.args.enforcer.KubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
			if err != nil {
				t.Errorf("Cannot create node %s : %v", node.Name, err)
			}
		}

		tt.args.enforcer.RegisterStrategyType(tt.d)
		tt.args.enforcer.AddStrategy(tt.d, tt.d.StrategyType())
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.d.Enforce(tt.args.enforcer, tt.args.cache); (err != nil) != tt.wantErr {
				if !strings.Contains(err.Error(), tt.wantErrMessageToken) {
					t.Errorf("Expecting output to match wantErr %v, instead got %v", tt.wantErrMessageToken, err)

					return
				}
				t.Errorf("Unexpected exception while trying to call Enforce  %v", err)

				return
			}

			if !tt.wantErr {
				// violating nodes
				labeledNodes, err := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "deschedule-test=violating"})
				if err != nil && !tt.wantErr {
					t.Errorf("Unexpected exception while trying to fetch violating nodes  %v", err)

					return
				}
				assertViolatingNodes(t, labeledNodes, tt.want.labeledNodes)
				nodes, _ := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
				assertViolatingNodes(t, nodes, tt.want.nodes)
			}
		})
	}
}

func TestDescheduleStrategy_Cleanup(t *testing.T) {
	type args struct {
		enforcer *strategy.MetricEnforcer
		cache    cache.ReaderWriter
	}

	tests := []struct {
		name                string
		d                   *Strategy
		node                *v1.Node
		args                args
		wantErr             bool
		wantErrMessageToken string
		want                expected
	}{
		{name: "node with violating label",
			d: &Strategy{PolicyName: "deschedule-test", Rules: []telpol.TASPolicyRule{
				{Metricname: "memory", Operator: "GreaterThan", Target: 1},
				{Metricname: "cpu", Operator: "LessThan", Target: 10}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"deschedule-test": "violating", "node-1-label": "test"}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()),
				cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{
				nodes:        map[string]map[string]string{"node-1": {"node-1-label": "test"}},
				labeledNodes: map[string]map[string]string{},
			}},
		{name: "node without violating label",
			d: &Strategy{PolicyName: "deschedule-test", Rules: []telpol.TASPolicyRule{
				{Metricname: "memory", Operator: "GreaterThan", Target: 1000},
				{Metricname: "cpu", Operator: "LessThan", Target: 10}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"deschedule-test": "", "node-2-label": "test"}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()),
				cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{
				nodes:        map[string]map[string]string{"node-2": {"deschedule-test": "", "node-2-label": "test"}},
				labeledNodes: map[string]map[string]string{},
			}},
		{name: "list nodes throws an error",
			d: &Strategy{PolicyName: "deschedule-test", Rules: []telpol.TASPolicyRule{
				{Metricname: "memory", Operator: "GreaterThan", Target: 1000},
				{Metricname: "cpu", Operator: "LessThan", Target: 10}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"deschedule-test": "", "test": "label"}}},
			args: args{enforcer: strategy.NewEnforcer(getClientWithListException()),
				cache: cache.MockEmptySelfUpdatingCache()},
			wantErr:             true,
			wantErrMessageToken: failNodeListCleanUpMessage,
			want:                expected{}},
		{name: "patch nodes throws an error",
			d: &Strategy{PolicyName: "deschedule-test", Rules: []telpol.TASPolicyRule{
				{Metricname: "memory", Operator: "GreaterThan", Target: 1000},
				{Metricname: "cpu", Operator: "LessThan", Target: 10}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"deschedule-test": "violating", "test": "label"}}},
			args: args{enforcer: strategy.NewEnforcer(getClientWithPatchException()),
				cache: cache.MockEmptySelfUpdatingCache()},
			wantErr:             false,
			wantErrMessageToken: failNodePatchMessage,
			want: expected{
				nodes:        map[string]map[string]string{"node-2": {"deschedule-test": "violating", "test": "label"}},
				labeledNodes: map[string]map[string]string{"node-2": {"deschedule-test": "violating", "test": "label"}},
			}},
	}

	for _, tt := range tests {
		tt := tt

		_, err := tt.args.enforcer.KubeClient.CoreV1().Nodes().Create(context.TODO(), tt.node, metav1.CreateOptions{})
		if err != nil {
			t.Errorf("Cannot write metric to mock cach for test: %v", err)
		}

		t.Run(tt.name, func(t *testing.T) {
			err := tt.d.Cleanup(tt.args.enforcer, tt.d.PolicyName)
			if (err != nil) != tt.wantErr {
				if !strings.Contains(err.Error(), tt.wantErrMessageToken) {
					t.Errorf("Expecting output to match wantErr %v, instead got %v", tt.wantErrMessageToken, err)

					return
				}
				t.Errorf("Strategy.Cleanup() unexpected error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr {
				labelledNodes, err := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "deschedule-test=violating"})
				if err != nil {
					if !tt.wantErr {
						t.Errorf("Strategy.Enforce() error = %v, wantErr %v", err, tt.wantErr)

						return
					}
					t.Errorf("Unexpected error encountered while trying to filter for the deschedule-test=violating label...")

					return
				}
				assertViolatingNodes(t, labelledNodes, tt.want.labeledNodes)
				nodes, _ := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
				assertViolatingNodes(t, nodes, tt.want.nodes)
			}
		})
	}
}
