package core

import (
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	telemetrypolicy "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	"log"
	"reflect"
	"testing"
	"time"
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
		{"LessThan true", args{100, telemetrypolicy.TASPolicyRule{"memory", "LessThan", 1000}}, true},
		{"GreaterThan true", args{100000, telemetrypolicy.TASPolicyRule{"memory", "GreaterThan", 1}}, true},
		{"Equals true", args{1, telemetrypolicy.TASPolicyRule{"memory", "Equals", 1}}, true},
		{"LessThan false", args{10000, telemetrypolicy.TASPolicyRule{"memory", "LessThan", 10}}, false},
		{"GreaterThan false", args{1, telemetrypolicy.TASPolicyRule{"memory", "GreaterThan", 10000}}, false},
		{"Equals false", args{1, telemetrypolicy.TASPolicyRule{"memory", "Equals", 100}}, false},
	}
	for _, tt := range tests {
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
		want []nodeSortableMetric
	}{
		{"less than test", args{testNodeMetricCustomInfo([]string{"node A", "node B", "node C"}, []int64{100, 200, 10}), "LessThan"}, []nodeSortableMetric{{"node C", *resource.NewQuantity(10, resource.DecimalSI)}, {"node A", *resource.NewQuantity(100, resource.DecimalSI)}, {"node B", *resource.NewQuantity(200, resource.DecimalSI)}}},
		{"greater than test", args{testNodeMetricCustomInfo([]string{"node A", "node B", "node C"}, []int64{100, 200, 10}), "GreaterThan"}, []nodeSortableMetric{{"node B", *resource.NewQuantity(200, resource.DecimalSI)}, {"node A", *resource.NewQuantity(100, resource.DecimalSI)}, {"node C", *resource.NewQuantity(10, resource.DecimalSI)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OrderedList(tt.args.metricsInfo, tt.args.operator)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("OrderedList() = %v, want %v", got, tt.want)
			}
			log.Print(got, tt.want)
		},
		)
	}
}
