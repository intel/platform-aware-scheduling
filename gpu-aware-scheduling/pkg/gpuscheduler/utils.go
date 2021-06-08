package gpuscheduler

import (
	"strings"

	v1 "k8s.io/api/core/v1"
)

const (
	// resourcePrefix is the intel resource prefix.
	resourcePrefix = "gpu.intel.com/"
)

func containerRequests(pod *v1.Pod) []resourceMap {
	allResources := []resourceMap{}

	for _, container := range pod.Spec.Containers {
		rm := resourceMap{}

		for name, quantity := range container.Resources.Requests {
			resourceName := name.String()
			if strings.HasPrefix(resourceName, resourcePrefix) {
				value, _ := quantity.AsInt64()
				rm[resourceName] = value
			}
		}

		allResources = append(allResources, rm)
	}

	return allResources
}

func hasGPUResources(pod *v1.Pod) bool {
	if pod == nil {
		return false
	}

	for i := 0; i < len(pod.Spec.Containers); i++ {
		container := &pod.Spec.Containers[i]
		for name := range container.Resources.Requests {
			resourceName := name.String()
			if strings.HasPrefix(resourceName, resourcePrefix) {
				return true
			}
		}
	}

	return false
}

func isCompletedPod(pod *v1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return true
	}

	switch pod.Status.Phase {
	case v1.PodFailed:
		fallthrough
	case v1.PodSucceeded:
		return true
	case v1.PodPending:
		fallthrough
	case v1.PodRunning:
		fallthrough
	case v1.PodUnknown:
		fallthrough
	default:
		return false
	}
}
