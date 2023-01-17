// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	l2 = 2
)

// MetricEnforcer instruments behavior to register strategies and trigger their enforcement actions.
type MetricEnforcer struct {
	RegisteredStrategies map[string]map[Interface]interface{}
	KubeClient           kubernetes.Interface
	sync.RWMutex
}

// NewEnforcer returns an enforcer with the passed arguments and an empty strategy store.
func NewEnforcer(kubeClient kubernetes.Interface) *MetricEnforcer {
	return &MetricEnforcer{
		RegisteredStrategies: make(map[string]map[Interface]interface{}),
		KubeClient:           kubeClient,
	}
}

// RegisterStrategyType adds the type of strategy as top level entry in the registry.
func (e *MetricEnforcer) RegisterStrategyType(str Interface) {
	e.Lock()
	defer e.Unlock()
	e.RegisteredStrategies[str.StrategyType()] = map[Interface]interface{}{}
}

// IsRegistered checks to see if a passed strategy is already being enforced.
func (e *MetricEnforcer) IsRegistered(str string) bool {
	e.Lock()
	defer e.Unlock()
	_, ok := e.RegisteredStrategies[str]

	return ok
}

// UnregisterStrategyType removes the passed strategy from the registry if it exists there.
// If it doesn't exist it fails silently.
func (e *MetricEnforcer) UnregisterStrategyType(str Interface) {
	e.Lock()
	defer e.Unlock()
	delete(e.RegisteredStrategies, str.StrategyType())
}

// RegisteredStrategyTypes returns a slice of the names of strategy types currently registered with the enforcer.
func (e *MetricEnforcer) RegisteredStrategyTypes() []string {
	output := make([]string, 0)

	e.Lock()
	defer e.Unlock()

	for name := range e.RegisteredStrategies {
		output = append(output, name)
	}

	return output
}

// RemoveStrategy will take a strategy out of the enforcer if it's currently registered.
func (e *MetricEnforcer) RemoveStrategy(str Interface, strategyType string) {
	e.Lock()
	defer e.Unlock()

	for s := range e.RegisteredStrategies[strategyType] {
		if s.Equals(str) {
			delete(e.RegisteredStrategies[strategyType], s)
			msg := fmt.Sprintf("Removed %v: %v from strategy register", s.GetPolicyName(), strategyType)
			klog.V(l2).InfoS(msg, "component", "controller")
		}
	}

	if enf, ok := str.(Enforceable); ok {
		err := enf.Cleanup(e, str.GetPolicyName())
		if err != nil {
			msg := fmt.Sprintf("Failed to remove strategy: %v", err)
			klog.V(l2).InfoS(msg, "component", "controller")
		}
	}
}

// AddStrategy includes the specific strategy under its type in the strategy registry.
func (e *MetricEnforcer) AddStrategy(str Interface, strategyType string) {
	e.Lock()
	defer e.Unlock()

	for s := range e.RegisteredStrategies[strategyType] {
		if s.Equals(str) {
			msg := fmt.Sprintf("Duplicate strategy found. Not adding %v: %v to registry", s.GetPolicyName(), s.StrategyType())
			klog.V(l2).InfoS(msg, "component", "controller")

			return
		}
	}

	msg := fmt.Sprintf("Adding strategies: %v %v", str.StrategyType(), str.GetPolicyName())
	klog.V(l2).InfoS(msg, "component", "controller")

	if _, ok := e.RegisteredStrategies[strategyType]; ok {
		if _, ok := str.(Enforceable); ok {
			e.RegisteredStrategies[strategyType][str] = nil

			return
		}
	}
}

// EnforceRegisteredStrategies runs periodically, enforcing each of the registered strategy types in the registry.
func (e *MetricEnforcer) EnforceRegisteredStrategies(cache cache.Reader, timer time.Ticker) {
	for {
		<-timer.C

		for registeredType := range e.RegisteredStrategies {
			e.enforceStrategy(registeredType, cache)
		}
	}
}

// enforceStrategy specifically calls the Enforce method of each strategy in the registry under a given type.
func (e *MetricEnforcer) enforceStrategy(strategyType string, cache cache.Reader) {
	e.Lock()
	defer e.Unlock()

	strList, ok := e.RegisteredStrategies[strategyType]
	if ok {
		for str := range strList {
			if enf, ok := str.(Enforceable); ok {
				_, err := enf.Enforce(e, cache)
				if err != nil {
					log.Print("Strategy was not enforceable.", err.Error(), "component", "controller")
				}
			}
		}
	}
}
