package cache

import (
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	telemetrypolicy "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"time"
)

//Reader is the functionality to read metrics and policies from the cache
type Reader interface {
	ReadMetric(string) (metrics.NodeMetricsInfo, error)
	ReadPolicy(string, string) (telemetrypolicy.TASPolicy, error)
}

//Writer is the functionality to edit metrics (write and delete) and Policies in the cache
type Writer interface {
	WriteMetric(string, metrics.NodeMetricsInfo) error
	WritePolicy(string, string, telemetrypolicy.TASPolicy) error
	DeleteMetric(string) error
	DeletePolicy(string, string) error
}

//ReaderWriter holds the functionality to both read and write metrics and policies
type ReaderWriter interface {
	Reader
	Writer
}

//SelfUpdating describes functionality to periodically update all metrics in the metric cache.
type SelfUpdating interface {
	PeriodicUpdate(time.Ticker, metrics.Client, map[string]interface{})
}
