// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"os"

	"github.com/intel/platform-aware-scheduling/extender"
	"github.com/intel/platform-aware-scheduling/gpu-aware-scheduling/pkg/gpuscheduler"
	"k8s.io/klog/v2"
)

// build variables need to be globals
//
//nolint:gochecknoglobals
var (
	goVersion = "value is set during build"
	buildDate = "value is set during build"
	version   = "value is set during build"
)

const (
	l1 = klog.Level(1)
)

func main() {
	var (
		kubeConfig, port, certFile, keyFile, caFile, balancedRes string
		enableAllowlist, enableDenylist                          bool
	)

	flag.StringVar(&kubeConfig, "kubeConfig", "/root/.kube/config", "location of kubernetes config file")
	flag.StringVar(&port, "port", "9001", "port on which the scheduler extender will listen")
	flag.StringVar(&certFile, "cert", "/etc/kubernetes/pki/ca.crt", "cert file extender will use for authentication")
	flag.StringVar(&keyFile, "key", "/etc/kubernetes/pki/ca.key", "key file extender will use for authentication")
	flag.StringVar(&caFile, "cacert", "/etc/kubernetes/pki/ca.crt", "ca file extender will use for authentication")
	flag.BoolVar(&enableAllowlist, "enableAllowlist", false, "enable allowed GPUs annotation (csv list of names)")
	flag.BoolVar(&enableDenylist, "enableDenylist", false, "enable denied GPUs annotation (csv list of names)")
	flag.StringVar(&balancedRes, "balancedResource", "", "enable resource balacing within a node")
	klog.InitFlags(nil)
	flag.Parse()

	klog.V(l1).Infof("%s built on %s with go %s", version, buildDate, goVersion)

	kubeClient, _, err := extender.GetKubeClient(kubeConfig)
	if err != nil {
		klog.Error("couldn't get kube client, cannot continue: ", err.Error())
		os.Exit(1)
	}

	gasscheduler := gpuscheduler.NewGASExtender(kubeClient, enableAllowlist, enableDenylist, balancedRes)
	sch := extender.Server{Scheduler: gasscheduler}
	sch.StartServer(port, certFile, keyFile, caFile, false)
	klog.Flush()
}
