package dontschedule

import (
	"github.com/intel/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/core"
	v1 "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	"reflect"
	"testing"
	"time"
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
		{"One node violating", Strategy{"test name", []v1.TASPolicyRule{{"memory", "GreaterThan", 9}}}, args{cache.MockEmptySelfUpdatingCache()}, map[string]interface{}{"node-1": nil}},
		{"No nodes violating", Strategy{"test name", []v1.TASPolicyRule{{"memory", "GreaterThan", 11}}}, args{cache.MockEmptySelfUpdatingCache()}, map[string]interface{}{}},
		{"No metric found", Strategy{"test name", []v1.TASPolicyRule{{"mem", "GreaterThan", 9}}}, args{cache.MockEmptySelfUpdatingCache()}, map[string]interface{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.args.cache.WriteMetric("memory", metrics.NodeMetricsInfo{"node-1": {time.Now(), 1, *resource.NewQuantity(10, resource.DecimalSI)}})
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
