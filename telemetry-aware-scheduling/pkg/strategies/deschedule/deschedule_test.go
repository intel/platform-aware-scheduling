// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package deschedule

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/klog/v2"

	strategy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testclient "k8s.io/client-go/kubernetes/fake"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

type args struct {
	enforcer *strategy.MetricEnforcer
}

type testItemStruct struct {
	name  string
	d     *Strategy
	nodes []*v1.Node
	args  args
	want  []string
}

type testStruc []testItemStruct

func TestDeschedule_Cleanup(t *testing.T) {
	var tests = testStruc{
		// This test labels node-1 as 'violating'. The labels should be removed after policy deletion.
		{name: "one node as 'violating'",
			d: &Strategy{PolicyName: "deschedule-test"},
			nodes: []*v1.Node{nodeSpec("deschedule-test", "node-1", "violating"),
				nodeSpec("deschedule-test", "node-2", "null")},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset())},
			want: []string{}},
		// This test labels node-1 and node-2 as 'violating'. The labels should be removed after policy deletion.
		{name: "multiple nodes as 'violating'",
			d: &Strategy{PolicyName: "deschedule-test"},
			nodes: []*v1.Node{nodeSpec("deschedule-test", "node-1", "violating"),
				nodeSpec("deschedule-test", "node-2", "violating")},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset())},
			want: []string{}},
		// In this test node-1 and node-2 are unlabeled. No labels should be added after policy deletion.
		{name: "multiple nodes",
			d: &Strategy{PolicyName: "deschedule-test"},
			nodes: []*v1.Node{nodeSpec("deschedule-test", "node-1", ""),
				nodeSpec("deschedule-test", "node-2", "")},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset())},
			want: []string{}},
	}

	for _, tt := range tests {
		tt := tt
		nodeAction(t, tt, "create")
		t.Run(tt.name, func(t *testing.T) {
			err := tt.d.Cleanup(tt.args.enforcer, tt.d.PolicyName) // testing Cleanup()
			if err != nil {
				klog.InfoS(err.Error(), "component", "testing")
			}
			nodys, _ := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "deschedule-test=violating"})
			msg := fmt.Sprint(nodys.Items)
			klog.InfoS(msg, "component", "testing")
			if len(nodys.Items) != len(tt.want) {
				t.Errorf("Number of labelled nodes: %v. Expected %v - Test failed", len(nodys.Items), len(tt.want))
			}
		})
		nodeAction(t, tt, "delete")
	}
}

func TestDeschedule_Relabel_nodes(t *testing.T) {
	var tests = testStruc{
		// This test will relabel node-1 as 'violating' after being removed by policy deletion.
		{name: "one node as 'violating'",
			d: &Strategy{PolicyName: "deschedule-test"},
			nodes: []*v1.Node{nodeSpec("deschedule-test", "node-1", "violating"),
				nodeSpec("deschedule-test", "node-2", "null")},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset())},
			want: []string{"violating"}},
		// This test will relabel node-1 and node-2 as 'violating' after being removed by policy deletion.
		{name: "multiple nodes as 'violating'",
			d: &Strategy{PolicyName: "deschedule-test"},
			nodes: []*v1.Node{nodeSpec("deschedule-test", "node-1", "violating"),
				nodeSpec("deschedule-test", "node-2", "violating")},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset())},
			want: []string{"violating", "violating"}},
	}

	for _, tt := range tests {
		tt := tt
		nodeAction(t, tt, "create")
		t.Run(tt.name, func(t *testing.T) {
			err := tt.d.Cleanup(tt.args.enforcer, tt.d.PolicyName) // testing Cleanup()
			if err != nil {
				klog.InfoS(err.Error(), "component", "testing")
			}
			nodeAction(t, tt, "update")
			nodys, _ := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "deschedule-test=violating"})
			msg := fmt.Sprint(nodys.Items)
			klog.InfoS(msg, "component", "testing")
			if len(nodys.Items) != len(tt.want) {
				t.Errorf("Number of labelled nodes: %v. Expected %v - Test failed", len(nodys.Items), len(tt.want))
			}
			for n := range tt.want {
				label := nodys.Items[n].Labels[tt.d.PolicyName]
				if label != tt.want[n] {
					t.Errorf("Wrong label: %v. Expected %v - Test failed", len(nodys.Items), tt.want[n])
				}
			}
		})
		nodeAction(t, tt, "delete")
	}
}

func nodeSpec(policyName string, name string, value string) *v1.Node {
	return &v1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: map[string]string{policyName: value}}}
}

func nodeAction(t *testing.T, testItem testItemStruct, action string) {
	for n := range testItem.nodes {
		switch action {
		case "create":
			_, err := testItem.args.enforcer.KubeClient.CoreV1().Nodes().Create(context.TODO(), testItem.nodes[n], metav1.CreateOptions{})
			if err != nil {
				t.Errorf("Cannot %v nodes correctly: %v", action, err)
			}

			msg := fmt.Sprintf("Labelling %v with %v", testItem.nodes[n].Name, testItem.nodes[n].Labels[testItem.d.PolicyName])
			klog.InfoS(msg, "component", "testing")
		case "update":
			_, err := testItem.args.enforcer.KubeClient.CoreV1().Nodes().Update(context.TODO(), testItem.nodes[n], metav1.UpdateOptions{})
			if err != nil {
				t.Errorf("Cannot %v nodes correctly: %v", action, err)
			}

			msg := fmt.Sprintf("Labelling %v with %v", testItem.nodes[n].Name, testItem.nodes[n].Labels[testItem.d.PolicyName])
			klog.InfoS(msg, "component", "testing")
		case "delete":
			err := testItem.args.enforcer.KubeClient.CoreV1().Nodes().Delete(context.TODO(), testItem.nodes[n].Name, metav1.DeleteOptions{})
			if err != nil {
				t.Errorf("Cannot %v nodes correctly: %v", action, err)
			}

			klog.InfoS("Nodes deleted", "component", "testing")
		default:
			klog.Fatal("not right action for node request")
		}
	}
}
