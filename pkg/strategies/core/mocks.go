package core

import (
	"github.com/intel/telemetry-aware-scheduling/pkg/cache"
)

//Here we have a mock strategy that is used in the tests for the core and other packages.
type MockStrategy struct {
	StrategyTypeMock string
}

func (v *MockStrategy) Violated(cache cache.Reader) map[string]interface{} {
	return map[string]interface{}{}
}
func (v *MockStrategy) Enforce(enforcer *MetricEnforcer, cache cache.Reader) (int, error) {
	return 0, nil
}

func (v *MockStrategy) StrategyType() string {
	return v.StrategyTypeMock
}
func (v *MockStrategy) Equals(o Interface) bool {
	return v.StrategyType() == o.StrategyType()
}
func (v *MockStrategy) GetPolicyName() string {
	return "mock-policy"
}
func (v *MockStrategy) SetPolicyName( policyName string)  {
	return
}