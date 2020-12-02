package cache
//This file contains mock methods and objects which are used to test across the TAS packages.
import (
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	telemetrypolicy "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

//MockEmptySelfUpdatingCache returns auto updating cache
func MockEmptySelfUpdatingCache() ReaderWriter {
	n := NewAutoUpdatingCache()
	go n.PeriodicUpdate(*time.NewTicker(time.Second), metrics.NewDummyMetricsClient(map[string]metrics.NodeMetricsInfo{}), map[string]interface{}{})
	return n
}

//MockSelfUpdatingCache returns auto updating cache
func MockSelfUpdatingCache() *AutoUpdatingCache {
	n := MockEmptySelfUpdatingCache()
	_ = n.WriteMetric("dummyMetric1", TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{50, 30}))
	_ = n.WriteMetric("dummyMetric2", TestNodeMetricCustomInfo([]string{"node 1", "node2"}, []int64{100, 200}))
	_ = n.WriteMetric("dummyMetric3", TestNodeMetricCustomInfo([]string{"node Z", "node Y"}, []int64{8, 40000000}))
	return n.(*AutoUpdatingCache)
}

//TestNodeMetricCustomInfo returns the node metrics information
func TestNodeMetricCustomInfo(nodeNames []string, numbers []int64) metrics.NodeMetricsInfo {
	n := metrics.NodeMetricsInfo{}
	for i, name := range nodeNames {
		n[name] = metrics.NodeMetric{Value: *resource.NewQuantity(numbers[i], resource.DecimalSI), Window: time.Second, Timestamp: time.Unix(100, 1)}
	}
	return n
}

var mockPolicy = telemetrypolicy.TASPolicy{
	ObjectMeta: v1.ObjectMeta{Name: "mock-policy", Namespace: "default"},
}
var mockPolicy2 = telemetrypolicy.TASPolicy{
	ObjectMeta: v1.ObjectMeta{Name: "not-mock-policy", Namespace: "default"},
}
