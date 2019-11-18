//The deschedule strategy, violations conditions and enforcement behavior are defined in this package.
// When a node is violating the deschedule strategy, the enforcer labels it as violating.
//This label can then be used externally to act on the strategy violation.
package deschedule

import (
	"github.com/intel/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/core"
	v1 "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	"log"
	"reflect"
	"testing"
	"time"
)

func TestDescheduleStrategy_SetPolicyName(t *testing.T) {
	log.Print("entered in strategy ")
	type args struct {
		name string
	}
	tests := []struct {
		name string
		d    *Strategy
		args args
	}{
		{name: "set basic name", d: &Strategy{}, args: args{"test policy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.d.SetPolicyName(tt.args.name)
			if tt.d.PolicyName != tt.args.name {
				t.Error("Outcome didn't match expected result")
			}
		})
	}
}

func TestDescheduleStrategy_GetPolicyName(t *testing.T) {
	tests := []struct {
		name string
		d    *Strategy
		want string
	}{
		{name: "retrieve basic name", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{}}, want: "test name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.GetPolicyName(); got != tt.want {
				t.Errorf("Strategy.GetPolicyName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDescheduleStrategy_Equals(t *testing.T) {
	type args struct {
		other core.Interface
	}
	tests := []struct {
		name string
		d    *Strategy
		args args
		want bool
	}{
		{name: "Equal empty strategies", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{}}, args: args{&Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 50}}}}},
		{name: "Equal one rule per strategy", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{"memory", "GreaterThan", 50}}}, args: args{&Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 50}}}}, want: true},
		{name: "different number rules same order", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{"cpu", "Equals", 1}, {Metricname: "memory", Operator: "GreaterThan", Target: 50}}}, args: args{&Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 50}}}}},
		{name: "Not equal different number rules different order", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{"memory", "GreaterThan", 50}, {Metricname: "cpu", Operator: "Equals", Target: 1}}}, args: args{other: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{"memory", "GreaterThan", 50}}}}},
		{name: "Not equal different rule names", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{"cpu", "GreaterThan", 50}}}, args: args{&Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 50}}}}},
		{name: "Not equal different operator", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{"memory", "LessThan", 50}}}, args: args{&Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 50}}}}},
		{name: "Not equal different target", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{"memory", "GreaterThan", 10}}}, args: args{&Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 50}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.Equals(tt.args.other); got != tt.want {
				a, _ := tt.args.other.(*Strategy)
				log.Print(a)
				t.Errorf("Strategy.Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDescheduleStrategy_Violated(t *testing.T) {
	type args struct {
		cache cache.ReaderWriter
	}
	tests := []struct {
		name string
		d    *Strategy
		args args
		want map[string]interface{}
	}{
		{name: "One node violating", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 9}}}, args: args{cache.MockEmptySelfUpdatingCache()}, want: map[string]interface{}{"node-1": nil}},
		{name: "No nodes violating", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 11}}}, args: args{cache.MockEmptySelfUpdatingCache()}, want: map[string]interface{}{}},
		{name: "No metric found", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "mem", Operator: "GreaterThan", Target: 9}}}, args: args{cache.MockEmptySelfUpdatingCache()}, want: map[string]interface{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.args.cache.WriteMetric("memory", metrics.NodeMetricsInfo{"node-1": {time.Now(), 1, *resource.NewQuantity(10, resource.DecimalSI)}})
			if err != nil {
				log.Print(err)
			}
			if got := tt.d.Violated(tt.args.cache); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Strategy.Violated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDescheduleStrategy_StrategyType(t *testing.T) {
	tests := []struct {
		name string
		d    *Strategy
		want string
	}{
		{name: "basic type", d: &Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{}}, want: "deschedule"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.StrategyType(); got != tt.want {
				t.Errorf("Strategy.StrategyType() = %v, want %v", got, tt.want)
			}
		})
	}
}
