package extender

import (
	"net/http"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Scheduler has the capabilities needed to prioritize and filter nodes based on http requests.
type Scheduler interface {
	Bind(w http.ResponseWriter, r *http.Request)
	Prioritize(w http.ResponseWriter, r *http.Request)
	Filter(w http.ResponseWriter, r *http.Request)
}

// Server type wraps the implementation of the extender.
type Server struct {
	Scheduler
}

// TODO: These types are in the k8s.io/kubernetes/extender/api package
// Some import issue is making them tough to access, so they are reimplemented here pending a solution.

// HostPriority represents the priority of scheduling to a particular host, higher priority is better.
type HostPriority struct {
	// Name of the host
	Host string
	// Score associated with the host
	Score int
}

// HostPriorityList declares a []HostPriority type.
type HostPriorityList []HostPriority

// FailedNodesMap is needed by HTTP server response.
type FailedNodesMap map[string]string

// Args represents the arguments needed by the extender to Filter/Prioritize
// nodes for a pod.
type Args struct {
	// List of candidate nodes where the pod can be scheduled; to be populated
	// only if ExtenderConfig.NodeCacheCapable == false
	Nodes *v1.NodeList
	// List of candidate node names where the pod can be scheduled; to be
	// populated only if ExtenderConfig.NodeCacheCapable == true
	NodeNames *[]string
	// Pod being scheduled
	Pod v1.Pod
}

// FilterResult stores the result from extender to be sent as response.
type FilterResult struct {
	// Filtered set of nodes where the pod can be scheduled; to be populated
	// only if ExtenderConfig.NodeCacheCapable == false
	Nodes *v1.NodeList
	// Filtered set of nodes where the pod can be scheduled; to be populated
	// only if ExtenderConfig.NodeCacheCapable == true
	NodeNames *[]string
	// Filtered out nodes where the pod can't be scheduled and the failure messages
	FailedNodes FailedNodesMap
	// Error message indicating failure
	Error string
}

// BindingArgs represents the arguments to an extender for binding a pod to a node.
type BindingArgs struct {
	// PodName is the name of the pod being bound
	PodName string
	// PodNamespace is the namespace of the pod being bound
	PodNamespace string
	// PodUID is the UID of the pod being bound
	PodUID types.UID
	// Node selected by the scheduler
	Node string
}

// BindingResult stores the result from extender to be sent as response.
type BindingResult struct {
	// Error message indicating failure
	Error string
}
