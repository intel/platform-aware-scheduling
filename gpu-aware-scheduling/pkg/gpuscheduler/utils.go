package gpuscheduler

import (
	"strings"

	v1 "k8s.io/api/core/v1"
)

const (
	// resourcePrefix is the intel resource prefix.
	resourcePrefix = "gpu.intel.com/"
	pciGroupLabel  = "gpu.intel.com/pci-groups"
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

// addPCIGroupGPUs goes through the given cards map and if they are requested to be handled as groups, the
// group is added to the given cards map with value "grouped".
func addPCIGroupGPUs(node *v1.Node, cards map[string]string) {
	groupedCardsToAdd := map[string]string{}

	for cardName, value := range cards {
		// if whole pci group is wanted to be impacted, add the whole group to podGPUs
		if value == pciGroupValue {
			pciGroupGPUNums := getPCIGroup(node, cardName)
			for _, gpuNum := range pciGroupGPUNums {
				groupedCardsToAdd["card"+gpuNum] = "grouped"
			}
		}
	}

	for cardName, value := range groupedCardsToAdd {
		if _, ok := cards[cardName]; !ok {
			cards[cardName] = value
		}
	}
}

func labelWithoutTASNS(label string) (string, bool) {
	if strings.HasPrefix(label, tasNSPrefix) {
		parts := strings.Split(label, "/")
		if len(parts) == maxLabelParts {
			return parts[1], true
		}
	}

	return "", false
}

func isGPUInPCIGroup(gpuName, pciGroupGPUName string, node *v1.Node) bool {
	gpuNums := getPCIGroup(node, pciGroupGPUName)
	for _, gpuNum := range gpuNums {
		if gpuName == "card"+gpuNum {
			return true
		}
	}

	return false
}

// getPCIGroup returns the pci group as slice, for the given gpu name.
func getPCIGroup(node *v1.Node, gpuName string) []string {
	if pciGroups, ok := node.Labels[pciGroupLabel]; ok {
		slicedGroups := strings.Split(pciGroups, "_")
		for _, group := range slicedGroups {
			gpuNums := strings.Split(group, ".")
			for _, gpuNum := range gpuNums {
				if "card"+gpuNum == gpuName {
					return gpuNums
				}
			}
		}
	}

	return []string{}
}

func hasGPUCapacity(node *v1.Node) bool {
	if node == nil {
		return false
	}

	if quantity, ok := node.Status.Capacity[gpuPluginResource]; ok {
		numI915, _ := quantity.AsInt64()
		if numI915 > 0 {
			return true
		}
	}

	return false
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
