// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"os"

	"k8s.io/client-go/util/homedir"

	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/intel/platform-aware-scheduling/extender"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/controller"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/metrics"
	strategy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/deschedule"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/labeling"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	telemetrypolicyclient "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/client/v1alpha1"
	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetryscheduler"
	"k8s.io/klog/v2"

	"context"

	tascache "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
)

const l2 = 2

func main() {
	var kubeConfig, port, certFile, keyFile, caFile, syncPeriod string

	klog.InitFlags(nil)
	flag.StringVar(&kubeConfig, "kubeConfig", filepath.Join(homedir.HomeDir(), ".kube", "config"), "location of kubernetes config file")
	flag.StringVar(&port, "port", "9001", "port on which the scheduler extender will listen")
	flag.StringVar(&certFile, "cert", "/etc/kubernetes/pki/ca.crt", "cert file extender will use for authentication")
	flag.StringVar(&keyFile, "key", "/etc/kubernetes/pki/ca.key", "key file extender will use for authentication")
	flag.StringVar(&caFile, "cacert", "/etc/kubernetes/pki/ca.crt", "ca file extender will use for authentication")
	flag.StringVar(&syncPeriod, "syncPeriod", "5s", "length of time in seconds between metrics updates")
	flag.Parse()

	cache := tascache.NewAutoUpdatingCache()
	tscheduler := telemetryscheduler.NewMetricsExtender(cache)

	sch := extender.Server{Scheduler: tscheduler}
	go sch.StartServer(port, certFile, keyFile, caFile, false)
	tasController(kubeConfig, syncPeriod, cache)
	klog.Flush()
}

// tasController The controller load the TAS policy/strategies and places them into a local cache that is available
// to all TAS components. It also monitors the current state of policies.
func tasController(kubeConfig string, syncPeriod string, cache *tascache.AutoUpdatingCache) {
	defer func() {
		err := recover()
		if err != nil {
			klog.V(l2).InfoS("Recovered from runtime error", "component", "controller")
		}
	}()

	kubeClient, clientConfig, err := extender.GetKubeClient(kubeConfig)
	if err != nil {
		klog.V(l2).InfoS("Issue in getting client config", "component", "controller")
		klog.Exit(err.Error())
	}

	syncDuration, err := time.ParseDuration(syncPeriod)
	if err != nil {
		klog.V(l2).InfoS("Sync problems in Parsing", "component", "controller")
		klog.Exit(err.Error())
	}

	metricsClient := metrics.NewClient(clientConfig)

	telpolicyClient, _, err := telemetrypolicyclient.NewRest(*clientConfig)
	if err != nil {
		klog.V(l2).InfoS("Rest client access to telemetrypolicy CRD problem", "component", "controller")
		klog.Exit(err.Error())
	}

	metricTicker := time.NewTicker(syncDuration)

	initialData := map[string]interface{}{}
	go cache.PeriodicUpdate(*metricTicker, metricsClient, initialData)

	enforcerTicker := time.NewTicker(syncDuration)

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	enfrcr := strategy.NewEnforcer(kubeClient)
	cont := controller.TelemetryPolicyController{
		Interface: telpolicyClient,
		Writer:    cache,
		Enforcer:  enfrcr,
	}

	enfrcr.RegisterStrategyType(&deschedule.Strategy{})
	enfrcr.RegisterStrategyType(&scheduleonmetric.Strategy{})
	enfrcr.RegisterStrategyType(&dontschedule.Strategy{})
	enfrcr.RegisterStrategyType(&labeling.Strategy{})

	go cont.Run(ctx)
	go enfrcr.EnforceRegisteredStrategies(cache, *enforcerTicker)

	done := make(chan os.Signal, 1)
	catchInterrupt(done)
}

func catchInterrupt(done chan os.Signal) {
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done
	klog.V(l2).InfoS("Policy controller closed ", "component", "controller")
}
