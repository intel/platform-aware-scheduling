package main

import (
	tascache "github.com/intel/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/telemetry-aware-scheduling/pkg/controller"
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	strategy "github.com/intel/telemetry-aware-scheduling/pkg/strategies/core"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/deschedule"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	telemetrypolicyclient "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/client/v1alpha1"

	"context"
	"flag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func parseCLIFlags(kubeConfig *string, syncPeriod *string, cachePort *string) {
	flag.StringVar(kubeConfig, "kubeConfig", "/root/.kube/config", "location of kubernetes config file")
	flag.StringVar(syncPeriod, "syncPeriod", "5s", "length of time in seconds between metrics updates")
	flag.StringVar(cachePort, "cachePort", "8111", "enpoint at which cache server should be as accessible")
	flag.Parse()
}

func main() {
	var kubeConfig, syncPeriod, cachePort string
	parseCLIFlags(&kubeConfig, &syncPeriod, &cachePort)
	kubeClient, clientConfig, err := getkubeClient(kubeConfig)
	if err != nil {
		panic(err)
	}
	syncDuration, err := time.ParseDuration(syncPeriod)
	if err != nil {
		panic(err)
	}
	metricsClient := metrics.NewClient(clientConfig)
	telpolicyClient, _, err := telemetrypolicyclient.NewRest(*clientConfig)
	if err != nil {
		panic(err)
	}
	metricTicker := time.NewTicker(syncDuration)
	cache := tascache.NewAutoUpdatingCache()
	initialData := map[string]interface{}{}
	go cache.PeriodicUpdate(*metricTicker, metricsClient, initialData)
	go cache.Serve(cachePort)

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
	go cont.Run(ctx)
	go enfrcr.EnforceRegisteredStrategies(cache, *enforcerTicker)
	done := make(chan os.Signal, 1)
	catchInterrupt(done)
}

func getkubeClient(kubeConfig string) (kubernetes.Interface, *rest.Config, error) {
	clientConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Print("not in cluster - trying file-based configuration")
		clientConfig, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			return nil, nil, err
		}
	}
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, nil, err
	}
	return kubeClient, clientConfig, nil
}

func catchInterrupt(done chan os.Signal) {
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done
	log.Println("\nPolicy controller closed ")
}
