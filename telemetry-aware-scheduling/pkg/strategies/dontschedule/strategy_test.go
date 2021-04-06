package dontschedule

import (
	"reflect"
	"testing"
	"time"

	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	v1 "github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestDontScheduleStrategy_Violated(t *testing.T) {
	type args struct {
		cache cache.ReaderWriter
	}
	tests := []struct {
		name string
		d    Strategy
		args args
		want map[string]interface{}
	}{
		{name: "One node violating", d: Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 9}}}, args: args{cache: cache.MockEmptySelfUpdatingCache()}, want: map[string]interface{}{"node-1": nil}},
		{name: "No nodes violating", d: Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "memory", Operator: "GreaterThan", Target: 11}}}, args: args{cache: cache.MockEmptySelfUpdatingCache()}, want: map[string]interface{}{}},
		{name: "No metric found", d: Strategy{PolicyName: "test name", Rules: []v1.TASPolicyRule{{Metricname: "mem", Operator: "GreaterThan", Target: 9}}}, args: args{cache: cache.MockEmptySelfUpdatingCache()}, want: map[string]interface{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.args.cache.WriteMetric("memory", metrics.NodeMetricsInfo{"node-1": {Timestamp: time.Now(), Window: 1, Value: *resource.NewQuantity(10, resource.DecimalSI)}})
			if err != nil {
				panic(err)
			}
			if got := tt.d.Violated(tt.args.cache); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Strategy.Violated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDontScheduleStrategy_StrategyType(t *testing.T) {
	tests := []struct {
		name string
		d    Strategy
		want string
	}{
		{"get strategy type", Strategy{}, "dontschedule"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Strategy{}
			if got := d.StrategyType(); got != tt.want {
				t.Errorf("Strategy.StrategyType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDontScheduleStrategy_Equals(t *testing.T) {
	type args struct {
		in0 core.Interface
	}
	tests := []struct {
		name string
		d    Strategy
		args args
		want bool
	}{
		{"simple equality test ", Strategy{}, args{&Strategy{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Strategy{}
			if got := d.Equals(tt.args.in0); got != tt.want {
				t.Errorf("Strategy.Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDontScheduleStrategy_GetPolicyName(t *testing.T) {
	tests := []struct {
		name string
		d    Strategy
		want string
	}{
		{"get name", Strategy{}, "demo-policy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Strategy{}
			d.SetPolicyName("demo-policy")
			if got := d.GetPolicyName(); got != tt.want {
				t.Errorf("Strategy.GetPolicyName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDontScheduleStrategy_Enforce(t *testing.T) {
	type args struct {
		enforcer *core.MetricEnforcer
		cache    cache.ReaderWriter
	}
	tests := []struct {
		name    string
		d       Strategy
		args    args
		want    int
		wantErr bool
	}{
		{"simple enforce test", Strategy{}, args{&core.MetricEnforcer{}, &cache.AutoUpdatingCache{}}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Strategy{}
			got, err := d.Enforce(tt.args.enforcer, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("Strategy.Enforce() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Strategy.Enforce() = %v, want %v", got, tt.want)
			}
		})
	}
}
