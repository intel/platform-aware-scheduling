package cache

import (
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	telemetrypolicy "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"log"
	"reflect"
	"testing"
	"time"
)

func TestNodeMetricsCache_PeriodicUpdate(t *testing.T) {
	type args struct {
		client metrics.Client
	}
	tests := []struct {
		name          string
		args          args
		delay         time.Duration
		queriedName   string
		queriedNode   string
		updatedMetric metrics.NodeMetricsInfo
		wantErr       bool
	}{
		{"existing metric",
			args{metrics.NewDummyMetricsClient(metrics.InstanceOfMockMetricClientMap)},
			2 * time.Second, "dummyMetric1", "node A",
			metrics.TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{500, 300}), false},
		{"non existing metric",
			args{metrics.NewDummyMetricsClient(metrics.InstanceOfMockMetricClientMap)},
			2 * time.Second, "missing metric", "node A",
			metrics.TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{500, 300}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewAutoUpdatingCache()
			go n.PeriodicUpdate(*time.NewTicker(time.Second), tt.args.client, map[string]interface{}{})
			err := n.WriteMetric("dummyMetric1", nil)
			_ = n.WriteMetric("", nil)
			if err != nil {
				if tt.wantErr {
					return
				} else {
					t.Error(err)
				}
			}
			atStart, _ := n.ReadMetric(tt.queriedName)
			metrics.InstanceOfMockMetricClientMap[tt.queriedName] = tt.updatedMetric
			time.Sleep(tt.delay)
			atEnd, err := n.ReadMetric(tt.queriedName)
			if err != nil {
				if tt.wantErr {
					return
				} else {
					t.Error(err)
				}
			}
			if atStart[tt.queriedNode].Value == atEnd[tt.queriedNode].Value {
				log.Print(atStart[tt.queriedNode].Value, atEnd[tt.queriedNode].Value)
				t.Fail()
			}
		})
	}
}

func TestNodeMetricsCache_ReadMetric(t *testing.T) {
	type args struct {
		metricName string
	}
	tests := []struct {
		name    string
		n       ReaderWriter
		args    args
		want    metrics.NodeMetricsInfo
		wantErr bool
	}{
		{"existing metric", MockSelfUpdatingCache(), args{"dummyMetric1"}, metrics.TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{50, 30}), false},
		{"non existing metric", MockSelfUpdatingCache(), args{"non-existing metric"}, metrics.TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{50, 30}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.n.ReadMetric(tt.args.metricName)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("AutoUpdatingCache.ReadMetric() error = %v", err)
					return
				} else {
					return
				}
			} else {
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("AutoUpdatingCache.ReadMetric() = %v, deleted %v", got, tt.want)
				}
			}
		})
	}
}
func TestNodeMetricsCache_ReadPolicy(t *testing.T) {
	type args struct {
		policy telemetrypolicy.TASPolicy
	}
	tests := []struct {
		name    string
		n       ReaderWriter
		args    args
		want    telemetrypolicy.TASPolicy
		wantErr bool
	}{
		{"existing policy", MockSelfUpdatingCache(), args{mockPolicy}, mockPolicy, false},
		{"non existing policy", MockSelfUpdatingCache(), args{mockPolicy}, mockPolicy2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err1 := tt.n.WritePolicy(tt.args.policy.Namespace, tt.args.policy.Name, mockPolicy)
			got, err2 := tt.n.ReadPolicy(tt.want.Namespace, tt.want.Name)
			if err1 != nil || err2 != nil {
				if !tt.wantErr {
					t.Errorf("AutoUpdatingCache.ReadPolicy() error = %v / %v", err1, err2)
					return
				} else {
					return
				}
			} else {
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("AutoUpdatingCache.ReadPolicy() = %v, deleted %v", got, tt.want)
				}
			}
			if tt.wantErr {
				t.Errorf("no error fired")
			}
		})
	}
}

func TestNodeMetricsCache_DeletePolicy(t *testing.T) {
	type args struct {
		policy telemetrypolicy.TASPolicy
	}
	tests := []struct {
		name    string
		n       ReaderWriter
		args    args
		deleted telemetrypolicy.TASPolicy
		wantErr bool
	}{
		{"delete existing policy", MockSelfUpdatingCache(), args{mockPolicy}, mockPolicy, true},
		{"delete non existing policy", MockSelfUpdatingCache(), args{mockPolicy}, mockPolicy2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = tt.n.WritePolicy(tt.args.policy.Namespace, tt.args.policy.Name, mockPolicy)
			err2 := tt.n.DeletePolicy(tt.deleted.Namespace, tt.deleted.Name)
			_, err3 := tt.n.ReadPolicy(tt.args.policy.Namespace, tt.args.policy.Name)
			if err2 != nil || err3 != nil {
				if !tt.wantErr {
					t.Errorf("AutoUpdatingCache.DeletePolicy() error = %v", err2)
					return
				} else {
					return
				}
			}
			if tt.wantErr {
				t.Errorf("no error fired")
			}
		})
	}
}
func TestNodeMetricsCache_WriteMetric(t *testing.T) {
	type args struct {
		metricName string
	}
	tests := []struct {
		name          string
		n             ReaderWriter
		queriedMetric string
		args          args
		errorExpected bool
	}{
		{"correct name", MockEmptySelfUpdatingCache(), "memory_free", args{"memory_free"}, false},
		{"false name queried", MockEmptySelfUpdatingCache(), "memory_free", args{"memoryFREE"}, true},
		{"number queried", MockEmptySelfUpdatingCache(), "1", args{"memoryFREE"}, true},
		{"add existing metric", MockEmptySelfUpdatingCache(), "dummyMetric1", args{"dummyMetric1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.n.WriteMetric(tt.args.metricName, nil)
			_, err = tt.n.ReadMetric(tt.queriedMetric)
			if err == nil && tt.errorExpected {
				t.Fail()
			}
		})
	}
}

func TestNodeMetricsCache_DeleteMetric(t *testing.T) {
	type args struct {
		metricName string
	}

	tests := []struct {
		name          string
		n             ReaderWriter
		args          args
		queriedMetric string
		expected      bool
	}{
		{"delete Existing Metric", MockSelfUpdatingCache(), args{"dummyMetric1"}, "dummyMetric1", false},
		{"delete all lower case", MockSelfUpdatingCache(), args{"dummymetric1"}, "dummyMetric1", true},
		{"delete non-Existing Metric", MockSelfUpdatingCache(), args{"top speed"}, "dummyMetric1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, presentAtStart := tt.n.ReadMetric(tt.queriedMetric)
			err := tt.n.DeleteMetric(tt.args.metricName)
			if err != nil {
				t.Error(err)
			}
			_, presentAtEnd := tt.n.ReadMetric(tt.queriedMetric)
			if presentAtStart != nil && presentAtEnd != nil {
				t.Fail()
			}
		})
	}
}
