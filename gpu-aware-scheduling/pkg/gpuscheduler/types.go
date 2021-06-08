package gpuscheduler

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// mock types

// ClientAPI is the mocked interface for the part of go client API used by the scheduler.
type ClientAPI interface {
	InClusterConfig() (*rest.Config, error)
	NewForConfig(*rest.Config) (kubernetes.Interface, error)
	UpdatePod(kubernetes.Interface, *v1.Pod) (*v1.Pod, error)
	GetPod(clientset kubernetes.Interface, ns, name string) (*v1.Pod, error)
}

// CacheAPI is the mocked interface for the Cache used by the scheduler.
type CacheAPI interface {
	NewCache(kubernetes.Interface) *Cache
	FetchNode(cache *Cache, nodeName string) (*v1.Node, error)
	GetNodeResourceStatus(cache *Cache, nodeName string) nodeResources
}

// InternalCacheAPI has the mocked interface of Cache internals.
type InternalCacheAPI interface {
	WaitForCacheSync(stopCh <-chan struct{}, cacheSyncs ...cache.InformerSynced) bool
}
