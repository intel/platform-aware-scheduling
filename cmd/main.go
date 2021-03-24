package main

import (
	"flag"
	"github.com/intel/telemetry-aware-scheduling/pkg/controller"
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	"github.com/intel/telemetry-aware-scheduling/pkg/scheduler"
	"github.com/intel/telemetry-aware-scheduling/pkg/telemetryscheduler"
	strategy "github.com/intel/telemetry-aware-scheduling/pkg/strategies/core"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/deschedule"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/dontschedule"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/scheduleonmetric"
	telemetrypolicyclient "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/client/v1alpha1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"context"
	tascache "github.com/intel/telemetry-aware-scheduling/pkg/cache"
)

func main() {
	var kubeConfig, port, certFile, keyFile, caFile, syncPeriod string
	var unsafe bool
	flag.StringVar(&kubeConfig, "kubeConfig", "/root/.kube/config", "location of kubernetes config file")
	flag.StringVar(&port, "port", "9001", "port on which the scheduler extender will listen")
	flag.StringVar(&certFile, "cert", "/etc/kubernetes/pki/ca.crt", "cert file extender will use for authentication")
	flag.StringVar(&keyFile, "key", "/etc/kubernetes/pki/ca.key", "key file extender will use for authentication")
	flag.StringVar(&caFile, "cacert", "/etc/kubernetes/pki/ca.crt", "ca file extender will use for authentication")
	flag.BoolVar(&unsafe, "unsafe", false, "unsafe instances of telemetry aware scheduler will be served over simple http.")
	flag.StringVar(&syncPeriod, "syncPeriod", "5s", "length of time in seconds between metrics updates")
	flag.Parse()
	cache := tascache.NewAutoUpdatingCache()
	tscheduler  := telemetryscheduler.NewMetricsExtender(cache)
	sch := scheduler.Server{ExtenderScheduler: tscheduler}
	go sch.StartServer(port, certFile, keyFile, caFile, unsafe)
	tasController(kubeConfig, syncPeriod, cache)
}

//tasController The controller load the TAS policy/strategies and places them into a local cache that is available
//to all TAS components. It also monitors the current state of policies.
func tasController(kubeConfig string, syncPeriod string, cache *tascache.AutoUpdatingCache) {
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
