// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package gpuscheduler

import (
	v1 "k8s.io/api/core/v1"
	kubernetes "k8s.io/client-go/kubernetes"
)

type cacheAPI struct{}

func (r *cacheAPI) NewCache(client kubernetes.Interface) *Cache {
	return NewCache(client)
}

func (r *cacheAPI) FetchNode(cache *Cache, nodeName string) (*v1.Node, error) {
	return cache.fetchNode(nodeName)
}

func (r *cacheAPI) FetchPod(cache *Cache, podNs, podName string) (*v1.Pod, error) {
	return cache.fetchPod(podNs, podName)
}

func (r *cacheAPI) GetNodeResourceStatus(cache *Cache, nodeName string) nodeResources {
	return cache.getNodeResourceStatus(nodeName)
}

func (r *cacheAPI) AdjustPodResourcesL(cache *Cache, pod *v1.Pod, adj bool, annotation,
	tileAnnotation, nodeName string,
) error {
	return cache.adjustPodResourcesL(pod, adj, annotation, tileAnnotation, nodeName)
}

func (r *cacheAPI) GetNodeTileStatus(cache *Cache, nodeName string) nodeTiles {
	return cache.getNodeTileStatus(nodeName)
}
