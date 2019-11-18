package core

import (
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	telempol "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sort"
)

//EvaluateRule returns a boolean after implementing the function described in the TASPolicyRule.
//The rule is transformed into a function inside of the method.
func EvaluateRule(value resource.Quantity, rule telempol.TASPolicyRule) bool {
	operators := map[string]func(resource.Quantity, int64) bool{
		"LessThan": func(value resource.Quantity, target int64) bool {
			return value.CmpInt64(target) == -1
		},
		"GreaterThan": func(value resource.Quantity, target int64) bool {
			return value.CmpInt64(target) == 1
		},
		"Equals": func(value resource.Quantity, target int64) bool {
			return value.CmpInt64(target) == 0
		},
	}
	return operators[rule.Operator](value, rule.Target)
}

//TODO: Make this method more generic so it can use objects other than nodes.
func OrderedList(metricsInfo metrics.NodeMetricsInfo, operator string) []nodeSortableMetric {
	mtrcs := []nodeSortableMetric{}
	for name, info := range metricsInfo {
		mtrcs = append(mtrcs, nodeSortableMetric{name, info.Value})
	}
	switch operator {
	case "GreaterThan":
		sort.Slice(mtrcs, func(i, j int) bool { return mtrcs[i].MetricValue.Cmp(mtrcs[j].MetricValue) == 1 })
	case "LessThan":
		sort.Slice(mtrcs, func(i, j int) bool { return mtrcs[i].MetricValue.Cmp(mtrcs[j].MetricValue) == -1 })
	}
	return mtrcs
}

//This type is necessary in order to call the sort.Slice method.
//Note lack of usage of time windows or stamps.
type nodeSortableMetric struct {
	NodeName    string
	MetricValue resource.Quantity
}
