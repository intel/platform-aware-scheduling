package main

import (
	"flag"
	"os"

	"github.com/intel/platform-aware-scheduling/extender"
	"github.com/intel/platform-aware-scheduling/gpu-aware-scheduling/pkg/gpuscheduler"
	"k8s.io/klog/v2"
)

func main() {
	var (
		kubeConfig, port, certFile, keyFile, caFile, balancedRes string
		enableAllowlist, enableDenylist, packResource            bool
	)

	flag.StringVar(&kubeConfig, "kubeConfig", "~/.kube/config", "location of kubernetes config file")
	flag.StringVar(&port, "port", "9001", "port on which the scheduler extender will listen")
	flag.StringVar(&certFile, "cert", "/etc/kubernetes/pki/ca.crt", "cert file extender will use for authentication")
	flag.StringVar(&keyFile, "key", "/etc/kubernetes/pki/ca.key", "key file extender will use for authentication")
	flag.StringVar(&caFile, "cacert", "/etc/kubernetes/pki/ca.crt", "ca file extender will use for authentication")
	flag.BoolVar(&enableAllowlist, "enableAllowlist", false, "enable allowed GPUs annotation (csv list of names)")
	flag.BoolVar(&enableDenylist, "enableDenylist", false, "enable denied GPUs annotation (csv list of names)")
	flag.StringVar(&balancedRes, "balancedResource", "", "enable resource balacing within a node")
	flag.BoolVar(&packResource, "packResource", false, "enable resource packed within one gpu card for pod")
	klog.InitFlags(nil)
	flag.Parse()

	kubeClient, _, err := extender.GetKubeClient(kubeConfig)
	if err != nil {
		klog.Error("couldn't get kube client, cannot continue: ", err.Error())
		os.Exit(1)
	}

	gasscheduler := gpuscheduler.NewGASExtender(kubeClient, enableAllowlist, enableDenylist, balancedRes, packResource)
	sch := extender.Server{Scheduler: gasscheduler}
	sch.StartServer(port, certFile, keyFile, caFile, false)
	klog.Flush()
}
