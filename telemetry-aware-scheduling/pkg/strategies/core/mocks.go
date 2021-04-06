package core

import (
	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
)

//MockStrategy is used in the tests for the core and other packages.
type MockStrategy struct {
	StrategyTypeMock string
}

//Violated gets the cache values from MockStrategy and returns the map interface
func (v *MockStrategy) Violated(cache cache.Reader) map[string]interface{} {
	return map[string]interface{}{}
}

//Enforce returns 0 value and nil error
func (v *MockStrategy) Enforce(enforcer *MetricEnforcer, cache cache.Reader) (int, error) {
	return 0, nil
}

//StrategyType returns the Strategy type of the mock
func (v *MockStrategy) StrategyType() string {
	return v.StrategyTypeMock
}

//Equals returns the value of checking if the strategy types are the same.
func (v *MockStrategy) Equals(o Interface) bool {
	return v.StrategyType() == o.StrategyType()
}

//GetPolicyName gets the mock strategy policy name
func (v *MockStrategy) GetPolicyName() string {
	return "mock-policy"
}

//SetPolicyName sets the policy name of the mock strategy
func (v *MockStrategy) SetPolicyName(policyName string) {

}
