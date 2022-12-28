// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package controller provides a controller that can be used to watch policies in the Kubernetes API.
// It registers strategies from those policies to an enforcer.
package controller

import (
	"context"
	"errors"
	"fmt"
	"log"

	strategy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/deschedule"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/labeling"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	telemetrypolicy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	l2 = 2
	l4 = 4
)

var errStrategyType = errors.New("provided strategy type is invalid")

// Run starts the controller watching on the Informer queue and doesnt' stop it,
// until the Done signal is received from context.
func (controller *TelemetryPolicyController) Run(context context.Context) {
	log.Print("Watching Telemetry Policies", "component", "controller")

	defer func() {
		err := recover()
		if err != nil {
			log.Print("Recovered from runtime error", "component", "controller")
		}
	}()

	_, err := controller.watch(context)

	if err != nil {
		log.Panic(err.Error())
	}

	<-context.Done()
}

// Watch sets up the watcher on the kubernetes api server and adds event handlers for add, update and delete.
func (controller *TelemetryPolicyController) watch(context context.Context) (cache.Controller, error) {
	source := cache.NewListWatchFromClient(
		controller,
		telemetrypolicy.Plural,
		core.NamespaceAll,
		fields.Everything(),
	)
	_, policyController := cache.NewInformer(
		source,
		&telemetrypolicy.TASPolicy{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.onAdd,
			UpdateFunc: controller.onUpdate,
			DeleteFunc: controller.onDelete,
		},
	)

	go policyController.Run(context.Done())

	return policyController, nil
}

// onAdd fires when the controller sees a new policy in the apiserver.
// It adds the policy to the cache and adds each of its metrics to the cache.
// It also adds the strategies contained in the policy to the strategy enforcer.
func (controller *TelemetryPolicyController) onAdd(obj interface{}) {
	pol, ok := obj.(*telemetrypolicy.TASPolicy)
	if !ok {
		klog.V(l4).InfoS("cannot add policy: not recognized as a telemetry policy", "component", "controller")

		return
	}

	polCopy := pol.DeepCopy()

	err := controller.WritePolicy(polCopy.Namespace, polCopy.Name, *polCopy)
	if err != nil {
		klog.V(l2).InfoS("Policy not added to cache: "+err.Error(), "component", "controller")

		return
	}

	for name := range polCopy.Spec.Strategies {
		klog.V(l4).InfoS("registering "+name+" from "+pol.Name, "component", "controller")
		strt, err := castStrategy(name, polCopy.Spec.Strategies[name])

		if err != nil {
			klog.V(l2).InfoS(err.Error(), "component", "controller")

			return
		}

		strt.SetPolicyName(polCopy.ObjectMeta.Name)
		controller.Enforcer.AddStrategy(strt, name)

		ruleset := polCopy.Spec.Strategies

		for _, rule := range ruleset[name].Rules {
			err := controller.WriteMetric(rule.Metricname, nil)
			if err == nil {
				klog.V(l2).InfoS("Added "+rule.Metricname, "component", "controller")
			}
		}
	}

	klog.V(l2).InfoS("Added policy, "+polCopy.Name, "component", "controller")
}

// castStrategy takes in a TASpolicy and returns its specific type based on the structure of the policy file.
func castStrategy(strategyType string, policy telemetrypolicy.TASPolicyStrategy) (strategy.Interface, error) {
	switch strategyType {
	case scheduleonmetric.StrategyType:
		str := (scheduleonmetric.Strategy)(policy)

		return &str, nil
	case deschedule.StrategyType:
		str := (deschedule.Strategy)(policy)

		return &str, nil
	case dontschedule.StrategyType:
		str := (dontschedule.Strategy)(policy)

		return &str, nil
	case labeling.StrategyType:
		str := (labeling.Strategy)(policy)

		return &str, nil
	default:
		return nil, fmt.Errorf("cast strategy failed: %w", errStrategyType)
	}
}

// onUpdate deletes the old policy and unregisters strategies and metrics.
func (controller *TelemetryPolicyController) onUpdate(older, newer interface{}) {
	oldPol := older.(*telemetrypolicy.TASPolicy)
	newPol := newer.(*telemetrypolicy.TASPolicy)
	polCopy := newPol.DeepCopy()

	err := controller.WritePolicy(polCopy.Namespace, polCopy.Name, *polCopy)
	if err != nil {
		msg := fmt.Sprintf("cached policy not updated %v", err)
		klog.V(l2).InfoS(msg, "component", "controller")

		return
	}

	klog.V(l2).InfoS("Policy: "+polCopy.Name+" updated", "component", "controller")

	for name := range polCopy.Spec.Strategies {
		oldStrat, err := castStrategy(name, oldPol.Spec.Strategies[name])
		if err != nil {
			klog.V(l2).InfoS(err.Error(), "component", "controller")

			return
		}

		oldStrat.SetPolicyName(polCopy.ObjectMeta.Name)
		controller.Enforcer.RemoveStrategy(oldStrat, oldStrat.StrategyType())

		for _, rule := range oldPol.Spec.Strategies[oldStrat.StrategyType()].Rules {
			errm := controller.DeleteMetric(rule.Metricname)
			if errm != nil {
				klog.V(l2).InfoS(errm.Error(), "component", "controller")
			}
		}

		strt, err := castStrategy(name, polCopy.Spec.Strategies[name])
		if err != nil {
			klog.V(l2).InfoS(err.Error(), "component", "controller")

			return
		}

		strt.SetPolicyName(polCopy.ObjectMeta.Name)
		controller.Enforcer.AddStrategy(strt, name)

		for _, rule := range polCopy.Spec.Strategies[name].Rules {
			err := controller.WriteMetric(rule.Metricname, nil)
			if err != nil {
				klog.V(l2).InfoS(err.Error(), "component", "controller")
			}
		}
	}
}

// onDelete gets rid of the policy along with its associated registered strategies and the metrics associated with them.
func (controller *TelemetryPolicyController) onDelete(obj interface{}) {
	pol := obj.(*telemetrypolicy.TASPolicy)
	polCopy := pol.DeepCopy()

	for name := range polCopy.Spec.Strategies {
		strt, err := castStrategy(name, polCopy.Spec.Strategies[name])
		if err != nil {
			klog.V(l2).InfoS(err.Error(), "component", "controller")

			return
		}

		strt.SetPolicyName(pol.Name)
		controller.Enforcer.RemoveStrategy(strt, strt.StrategyType())

		for _, rule := range polCopy.Spec.Strategies[strt.StrategyType()].Rules {
			err := controller.DeleteMetric(rule.Metricname)
			if err != nil {
				klog.V(l2).InfoS(err.Error(), "component", "controller")
			}
		}
	}

	err := controller.DeletePolicy(polCopy.Namespace, polCopy.Name)
	if err != nil {
		klog.V(l4).InfoS(err.Error(), "component", "controller")

		return
	}

	klog.V(l2).InfoS("Policy: "+polCopy.Name+" deleted", "component", "controller")
}
