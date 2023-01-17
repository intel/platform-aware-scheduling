// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

//go:build validation
// +build validation

// this test exists for running the scheduler extender with code coverage enabled in a real cluster with
// a set of validation tests. Effectively the "TestValidation" has the gist of the main() function in it,
// albeit with catching of panics and with a little pre-stop server in order to exit the test function
// properly with the coverage written out. Pre-stop gets called when the extender is deleted, for example
// when the extender is switched to a non-coverage-build container

package gpuscheduler

import (
	"flag"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/intel/platform-aware-scheduling/extender"
	"k8s.io/klog/v2"
)

var (
	kubeConfig = flag.String("kubeConfig", "/root/.kube/config", "location of kubernetes config file")
	certFile   = flag.String("cert", "/etc/kubernetes/pki/ca.crt", "cert file extender will use for authentication")
	port       = flag.String("port", "9001", "port on which the scheduler extender will listen")
	keyFile    = flag.String("key", "/etc/kubernetes/pki/ca.key", "key file extender will use for authentication")
	caFile     = flag.String("cacert", "/etc/kubernetes/pki/ca.crt", "ca file extender will use for authentication")
	unsafe     = flag.Bool("unsafe", false, "unsafe instances of gpu aware scheduler will be served over simple http.")
)

func init() {
	klog.InitFlags(flag.CommandLine)
}

func TestValidation(t *testing.T) {
	klog.V(2).Info("klog level 2 enabled")
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered,", r)
		}
	}()

	kubeClient, _, err := extender.GetKubeClient(*kubeConfig)
	if err != nil {
		panic(err)
	}

	gasscheduler := NewGASExtender(kubeClient, true, true)
	sch := extender.Server{Scheduler: gasscheduler}
	c := make(chan bool)
	go preStopServer(c)
	go sch.StartServer(*port, *certFile, *keyFile, *caFile, *unsafe)
	<-c
}

func preStop(w http.ResponseWriter, r *http.Request, c chan bool) {
	klog.Info("pre-stop called")
	c <- true
	time.Sleep(time.Second)
	w.WriteHeader(http.StatusOK)
}

func preStopServer(c chan bool) {
	http.HandleFunc("/prestop", func(w http.ResponseWriter, r *http.Request) { preStop(w, r, c) })
	port := "8088"
	klog.V(1).Infof("Test Listening for pre-stop-hook on HTTP %v", port)
	http.ListenAndServe(":"+port, nil)
}
