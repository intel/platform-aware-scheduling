// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// core pkg contains the behaviour shared between all Violable and Enforceable strategies from a Telemetry Policy.

package core

import (
	"time"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
)

// Interface describes expected behavior of a specific strategy.
type Interface interface {
	Violated(cache cache.Reader) map[string]interface{}
	StrategyType() string
	Equals(obj Interface) bool
	GetPolicyName() string
	SetPolicyName(policyName string)
}

// Enforceable enforce strategies and clean up after strategies are removed.
type Enforceable interface {
	Enforce(enforcer *MetricEnforcer, cache cache.Reader) (int, error)
	Cleanup(enforcer *MetricEnforcer, policyName string) error
}

// Enforcer registers strategies by type, adds specific strategies to a registry, and Enforces those strategies.
type Enforcer interface {
	RegisterStrategyType(strategy Interface)
	UnregisterStrategyType(strategy Interface)
	IsRegistered(strategy string) bool
	AddStrategy(strategy Interface, strategyType string)
	RemoveStrategy(strategy Interface, strategyType string)
	EnforceRegisteredStrategies(cache cache.Reader, ticker time.Ticker)
}
