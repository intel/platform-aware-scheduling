//Package controller provides a controller that can be used to watch policies in the Kubernetes API.
//It registers strategies from those policies to an enforcer.
package controller

import (
	"context"
	"errors"
	strategy "github.com/intel/telemetry-aware-scheduling/pkg/strategies/core"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/deschedule"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	telemetrypolicy "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"
	"log"
)

//Run starts the controller watching on the Informer queue and doesnt' stop it until the Done signal is received from context
func (controller *TelemetryPolicyController) Run(context context.Context) {
	log.Print("Watching Telemetry Policies ")
	_, err := controller.watch(context)
	if err != nil {
		panic(err)
	}
	<-context.Done()
}

//Watch sets up the watcher on the kubernetes api server and adds event handlers for add, update and delete.
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

//onAdd fires when the controller sees a new policy in the apiserver. It adds the policy to the cache and adds each of its metrics to the cache.
// It also adds the strategies contained in the policy to the strategy enforcer.
func (controller *TelemetryPolicyController) onAdd(obj interface{}) {
	pol, ok := obj.(*telemetrypolicy.TASPolicy)
	if !ok {
		log.Print("cannot add policy: not recognized as a telemetry policy")
		return
	}
	polCopy := pol.DeepCopy()
	err := controller.WritePolicy(polCopy.Namespace, polCopy.Name, *polCopy)
	if err != nil {
		log.Print("policy not added to cache: ", err)
		return
	}
	for _, name := range controller.Enforcer.RegisteredStrategyTypes() {
		log.Printf("registering %v from %v", name, pol.Name)
		strt, err := castStrategy(name, polCopy.Spec.Strategies[name])
		if err != nil {
			log.Print(err)
			return
		}
		strt.SetPolicyName(polCopy.ObjectMeta.Name)
		controller.Enforcer.AddStrategy(strt, name)
		ruleset := polCopy.Spec.Strategies
		for _, rule := range ruleset[name].Rules {
			err := controller.WriteMetric(rule.Metricname, nil)
			if err == nil {
				log.Print("Added " + rule.Metricname)
			}
		}
	}
	log.Println("Added policy,", polCopy.Name)
}

//castStrategy takes in a TASpolicy and returns its specific type based on the structure of the policy file.
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
	default:
		return nil, errors.New("strategy could not be added - invalid strategy type")
	}
}

//Update deletes the old policy and unregisters strategies and metrics
func (controller *TelemetryPolicyController) onUpdate(old, new interface{}) {
	oldPol := old.(*telemetrypolicy.TASPolicy)
	newPol := new.(*telemetrypolicy.TASPolicy)
	polCopy := newPol.DeepCopy()
	err := controller.WritePolicy(polCopy.Namespace, polCopy.Name, *polCopy)
	if err != nil {
		log.Print("cached policy not updated: ", err)
		return
	}
	log.Println("Policy: ", polCopy.Name, " updated")
	for _, name := range controller.Enforcer.RegisteredStrategyTypes() {
		oldStrat, err := castStrategy(name, oldPol.Spec.Strategies[name])
		if err != nil {
			log.Print(err)
			return
		}
		controller.Enforcer.RemoveStrategy(oldStrat, oldStrat.StrategyType())
		for _, rule := range oldPol.Spec.Strategies[oldStrat.StrategyType()].Rules {
			err := controller.DeleteMetric(rule.Metricname)
			if err != nil {
				log.Print(err)
			}
		}
		strt, err := castStrategy(name, polCopy.Spec.Strategies[name])
		if err != nil {
			log.Print(err)
			return
		}
		strt.SetPolicyName(polCopy.ObjectMeta.Name)
		controller.Enforcer.AddStrategy(strt, name)
		for _, rule := range polCopy.Spec.Strategies[name].Rules {
			err := controller.WriteMetric(rule.Metricname, nil)
			if err != nil {
				log.Print(err)
			}
		}
	}
}

//On delete gets rid of the policy along with its associated registered strategies and the metrics associated with them.
func (controller *TelemetryPolicyController) onDelete(obj interface{}) {
	pol := obj.(*telemetrypolicy.TASPolicy)
	polCopy := pol.DeepCopy()
	for _, name := range controller.Enforcer.RegisteredStrategyTypes() {
		strt, err := castStrategy(name, polCopy.Spec.Strategies[name])
		if err != nil {
			log.Print(err)
			return
		}
		strt.SetPolicyName(pol.Name)
		controller.Enforcer.RemoveStrategy(strt, strt.StrategyType())
		for _, rule := range polCopy.Spec.Strategies[strt.StrategyType()].Rules {
			err := controller.DeleteMetric(rule.Metricname)
			if err != nil {
				log.Print(err)
			}
		}
	}
	err := controller.DeletePolicy(polCopy.Namespace, polCopy.Name)
	if err != nil {
		log.Print(err)
		return
	}
	log.Println("Policy: ", polCopy.Name, " deleted")
}
