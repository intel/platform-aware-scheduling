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

func (r *cacheAPI) GetNodeResourceStatus(cache *Cache, nodeName string) nodeResources {
	return cache.getNodeResourceStatus(nodeName)
}
