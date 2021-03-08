package scheduler

import (
	v1 "k8s.io/api/core/v1"
	"net/http"
)

//ExtenderScheduler has the capabilities needed to prioritize and filter nodes based on http requests.
type ExtenderScheduler interface {
	Prioritize(w http.ResponseWriter, r *http.Request)
	Filter(w http.ResponseWriter, r *http.Request)
}

//Server type wraps the implementation of the scheduler.
type Server struct {
	ExtenderScheduler
}

//TODO: These types are in the k8s.io/kubernetes/scheduler/api package
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

// ExtenderArgs represents the arguments needed by the extender to Filter/Prioritize
// nodes for a pod.
type ExtenderArgs struct {
	// Pod being scheduled
	Pod v1.Pod
	// List of candidate nodes where the pod can be scheduled; to be populated
	// only if ExtenderConfig.NodeCacheCapable == false
	Nodes *v1.NodeList
	// List of candidate node names where the pod can be scheduled; to be
	// populated only if ExtenderConfig.NodeCacheCapable == true
	NodeNames *[]string
}

// ExtenderFilterResult stores the result from extender to be sent as response.
type ExtenderFilterResult struct {
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
