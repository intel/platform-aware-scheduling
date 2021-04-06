package deschedule

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	strategy "github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	telpol "github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testclient "k8s.io/client-go/kubernetes/fake"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func TestDescheduleStrategy_Enforce(t *testing.T) {
	type args struct {
		enforcer *strategy.MetricEnforcer
		cache    cache.ReaderWriter
	}
	type expected struct {
		nodeNames []string
	}
	tests := []struct {
		name    string
		d       *Strategy
		node    *v1.Node
		args    args
		wantErr bool
		want    expected
	}{
		{name: "node label test",
			d:    &Strategy{PolicyName: "deschedule-test", Rules: []telpol.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 1}, {Metricname: "cpu", Operator: "LessThan", Target: 10}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"deschedule-test": ""}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()),
				cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{"node-1"}}},
		{name: "node unlabel test",
			d:    &Strategy{PolicyName: "deschedule-test", Rules: []telpol.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 1000}, {Metricname: "cpu", Operator: "LessThan", Target: 10}}},
			node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"deschedule-test": "violating"}}},
			args: args{enforcer: strategy.NewEnforcer(testclient.NewSimpleClientset()),
				cache: cache.MockEmptySelfUpdatingCache()},
			want: expected{nodeNames: []string{}}},
	}
	for _, tt := range tests {
		err := tt.args.cache.WriteMetric("memory", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1, Value: *resource.NewQuantity(100, resource.DecimalSI)}})
		_, err = tt.args.enforcer.KubeClient.CoreV1().Nodes().Create(context.TODO(), tt.node, metav1.CreateOptions{})
		tt.args.enforcer.RegisterStrategyType(tt.d)
		tt.args.enforcer.AddStrategy(tt.d, tt.d.StrategyType())
		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			if _, err = tt.d.Enforce(tt.args.enforcer, tt.args.cache); (err != nil) != tt.wantErr {
				t.Errorf("Strategy.Enforce() error = %v, wantErr %v", err, tt.wantErr)
			}
			labelledNodes, err := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "deschedule-test=violating"})
			if err != nil {
				if !tt.wantErr {
					t.Errorf("Strategy.Enforce() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				return
			}
			for _, node := range labelledNodes.Items {
				got = append(got, node.Name)
			}
			nodys, _ := tt.args.enforcer.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			log.Print(nodys.Items[0])
			if len(tt.want.nodeNames) != len(got) {
				t.Errorf("Number of pods returned: %v not as expected: %v", got, tt.want.nodeNames)
			}
		})
	}
}
