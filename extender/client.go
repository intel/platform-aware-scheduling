// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package extender

import (
	"fmt"

	"k8s.io/klog/v2"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// GetKubeClient returns the kube client interface with its config.
func GetKubeClient(kubeConfig string) (kubernetes.Interface, *rest.Config, error) {
	return GetKubeClientExt(kubeConfig, 0, 0)
}

// GetKubeClientExt returns the kube client interface with its config. The given Burst and QPS are set into the config.
func GetKubeClientExt(kubeConfig string, burst int, qps float32) (kubernetes.Interface, *rest.Config, error) {
	clientConfig, err := rest.InClusterConfig()
	if err != nil {
		klog.V(l2).InfoS("not in cluster - trying file-based configuration", "component", "controller")

		clientConfig, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get clientconfig: %w", err)
		}
	}

	clientConfig.Burst = burst
	clientConfig.QPS = qps

	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kubeClientset %w", err)
	}

	return kubeClient, clientConfig, nil
}
