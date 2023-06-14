// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"time"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
)

// AddStrategyInvocation is used to test the times that AddStrategy is called.
type AddStrategyInvocation struct {
	I interface{}
	s string
}

// MockStrategy is used in the tests for the core and other packages.
type MockStrategy struct {
	StrategyTypeMock     string
	RegisteredStrategies map[string]map[Interface]interface{}
	AddedStrategies      AddStrategyInvocation
	RemovedStrategies    interface{}
}

// Violated gets the cache values from MockStrategy and returns the map interface.
func (v *MockStrategy) Violated(_ cache.Reader) map[string]interface{} {
	return map[string]interface{}{}
}

// Enforce returns 0 value and nil error.
func (v *MockStrategy) Enforce(_ *MetricEnforcer, _ cache.Reader) (int, error) {
	return 0, nil
}

// StrategyType returns the Strategy type of the mock.
func (v *MockStrategy) StrategyType() string {
	return v.StrategyTypeMock
}

// Equals returns the value of checking if the strategy types are the same.
func (v *MockStrategy) Equals(o Interface) bool {
	return v.StrategyType() == o.StrategyType()
}

// GetPolicyName gets the mock strategy policy name.
func (v *MockStrategy) GetPolicyName() string {
	return "core-mock-policy"
}

// SetPolicyName sets the policy name of the mock strategy.
func (v *MockStrategy) SetPolicyName(string) {

}

// Clean returns  nil error.
func (v *MockStrategy) Clean(*MetricEnforcer, string) error {
	return nil
}

// RegisterStrategyType is a method in Mock strategy.
func (v *MockStrategy) RegisterStrategyType(strategy Interface) {
	v.RegisteredStrategies[strategy.StrategyType()] = map[Interface]interface{}{}
}

// UnregisterStrategyType is a method in Mock strategy.
func (v *MockStrategy) UnregisterStrategyType(Interface) {

}

// IsRegistered is a method in Mock strategy.
func (v *MockStrategy) IsRegistered(strategy string) bool {
	return v.StrategyType() == strategy
}

// AddStrategy is a method in Mock strategy.
func (v *MockStrategy) AddStrategy(i Interface, s string) {
	v.AddedStrategies = AddStrategyInvocation{i, s}
}

// RemoveStrategy is a method in Mock strategy.
func (v *MockStrategy) RemoveStrategy(_ Interface, _ string) {
	v.RemovedStrategies = nil
}

// EnforceRegisteredStrategies is a method in Mock strategy.
func (v *MockStrategy) EnforceRegisteredStrategies(cache.Reader, time.Ticker) {

}
