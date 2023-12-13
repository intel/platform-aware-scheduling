// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package gpuscheduler

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// mock types

// CacheAPI is the mocked interface for the Cache used by the scheduler.
type CacheAPI interface {
	NewCache(client kubernetes.Interface) *Cache
	FetchNode(cache *Cache, nodeName string) (*v1.Node, error)
	FetchPod(cache *Cache, podNS, podName string) (*v1.Pod, error)
	GetNodeResourceStatus(cache *Cache, nodeName string) nodeResources
	GetNodeTileStatus(cache *Cache, nodeName string) nodeTiles
	AdjustPodResourcesL(cache *Cache, pod *v1.Pod, adj bool, annotation, tileAnnotation, nodeName string) error
}

// InternalCacheAPI has the mocked interface of Cache internals.
type InternalCacheAPI interface {
	WaitForCacheSync(stopCh <-chan struct{}, cacheSyncs ...cache.InformerSynced) bool
}
