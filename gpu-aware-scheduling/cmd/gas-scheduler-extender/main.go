package main

import (
	"flag"

	"github.com/intel/platform-aware-scheduling/extender"
	"github.com/intel/platform-aware-scheduling/gpu-aware-scheduling/pkg/gpuscheduler"
	"k8s.io/klog/v2"
)

func main() {
	var (
		kubeConfig, port, certFile, keyFile, caFile string
		unsafe                                      bool
	)

	flag.StringVar(&kubeConfig, "kubeConfig", "/root/.kube/config", "location of kubernetes config file")
	flag.StringVar(&port, "port", "9001", "port on which the scheduler extender will listen")
	flag.StringVar(&certFile, "cert", "/etc/kubernetes/pki/ca.crt", "cert file extender will use for authentication")
	flag.StringVar(&keyFile, "key", "/etc/kubernetes/pki/ca.key", "key file extender will use for authentication")
	flag.StringVar(&caFile, "cacert", "/etc/kubernetes/pki/ca.crt", "ca file extender will use for authentication")
	flag.BoolVar(&unsafe, "unsafe", false, "unsafe instances of GPU aware scheduler will be served over simple http.")
	klog.InitFlags(nil)
	flag.Parse()

	kubeClient, _, err := extender.GetKubeClient(kubeConfig)
	if err != nil {
		panic(err)
	}

	gasscheduler := gpuscheduler.NewGASExtender(kubeClient)
	sch := extender.Server{Scheduler: gasscheduler}
	sch.StartServer(port, certFile, keyFile, caFile, unsafe)
	klog.Flush()
}
