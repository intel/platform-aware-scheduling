// Package gpuscheduler has the logic for the scheduler extender - including the server it starts and filter methods
package gpuscheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/intel/platform-aware-scheduling/extender"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	tsAnnotationName        = "gas-ts"
	cardAnnotationName      = "gas-container-cards"
	tileAnnotationName      = "gas-container-tiles"
	allowlistAnnotationName = "gas-allow"
	denylistAnnotationName  = "gas-deny"
	samegpuAnnotationName   = "gas-same-gpu"
	samegpuMaxI915Request   = 1
	samegpuMinContainers    = 2
	tasNSPrefix             = "telemetry.aware.scheduling."
	gpuDisableLabelPrefix   = "gas-disable-"
	gpuPreferenceLabel      = "gas-prefer-gpu"
	tileDisableLabelPrefix  = "gas-tile-disable-"
	tileDeschedLabelPrefix  = "gas-tile-deschedule-"
	tilePrefLabelPrefix     = "gas-tile-preferred-"
	gpuPrefix               = "gpu.intel.com/"
	gpuListLabel            = gpuPrefix + "cards"
	gpuMonitoringResource   = gpuPrefix + "i915_monitoring"
	gpuNumbersLabel         = gpuPrefix + "gpu-numbers"
	gpuPluginResource       = gpuPrefix + "i915"
	gpuTileResource         = gpuPrefix + "tiles"
	l1                      = klog.Level(1)
	l2                      = klog.Level(2)
	l3                      = klog.Level(3)
	l4                      = klog.Level(4)
	l5                      = klog.Level(5)
	maxLabelParts           = 2
	base10                  = 10
)

//nolint: gochecknoglobals // only mocked APIs are allowed as globals
var (
	iCache CacheAPI
)

// Errors.
var (
	errNotFound    = errors.New("not found")
	errEmptyBody   = errors.New("request body empty")
	errDecode      = errors.New("error decoding request")
	errWontFit     = errors.New("will not fit")
	errExtractFail = errors.New("failed to extract value(s)")
	errBadUID      = errors.New("provided UID is incorrect")
	errAnnotation  = errors.New("malformed annotation")
	errResConflict = errors.New("resources conflict")
)

//nolint: gochecknoinits // only mocked APIs are allowed in here
func init() {
	iCache = &cacheAPI{}
}

// GASExtender is the scheduler extension part.
type GASExtender struct {
	clientset        kubernetes.Interface
	cache            *Cache
	balancedResource string
	rwmutex          sync.RWMutex
	allowlistEnabled bool
	denylistEnabled  bool
}

// NewGASExtender returns a new GAS Extender.
func NewGASExtender(clientset kubernetes.Interface, enableAllowlist,
	enableDenylist bool, balanceResource string) *GASExtender {
	return &GASExtender{
		cache:            iCache.NewCache(clientset),
		clientset:        clientset,
		allowlistEnabled: enableAllowlist,
		denylistEnabled:  enableDenylist,
		balancedResource: balanceResource,
	}
}

func (m *GASExtender) annotatePodBind(annotation, tileAnnotation string, pod *v1.Pod) error {
	var err error

	ts := strconv.FormatInt(time.Now().UnixNano(), base10)

	var payload []patchValue

	if pod.Annotations == nil {
		var empty struct{}

		payload = append(payload, patchValue{
			Op:    "add",
			Path:  "/metadata/annotations",
			Value: empty,
		})
	}

	payload = append(payload, patchValue{
		Op:    "add",
		Path:  "/metadata/annotations/" + tsAnnotationName,
		Value: ts,
	})

	payload = append(payload, patchValue{
		Op:    "add",
		Path:  "/metadata/annotations/" + cardAnnotationName,
		Value: annotation,
	})

	if tileAnnotation != "" {
		payload = append(payload, patchValue{
			Op:    "add",
			Path:  "/metadata/annotations/" + tileAnnotationName,
			Value: tileAnnotation,
		})
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		klog.Errorf("Json marshal failed for pod %v")

		return fmt.Errorf("pod %s annotation failed: %w", pod.GetName(), err)
	}

	_, err = m.clientset.CoreV1().Pods(pod.GetNamespace()).Patch(
		context.TODO(), pod.GetName(), types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
	if err == nil {
		klog.V(l2).Infof("Annotated pod %v with annotation %v", pod.GetName(), annotation)
	} else {
		klog.Errorf("Pod %s annotating failed. Err %v", pod.GetName(), err.Error())
		err = fmt.Errorf("pod %s annotation failed: %w", pod.GetName(), err)
	}

	return err
}

func getNodeGPUList(node *v1.Node) []string {
	if node == nil || node.Labels == nil {
		klog.Error("No labels in node")

		return nil
	}

	var cards = []string{}

	if gpuNumbersValue := concatenateSplitLabel(node, gpuNumbersLabel); gpuNumbersValue != "" {
		indexes := strings.Split(gpuNumbersValue, ".")
		cards = make([]string, 0, len(indexes))

		for _, index := range indexes {
			cards = append(cards, "card"+index)
		}
	}

	// Deprecated, remove after intel device plugins release-0.23 drops to unsupported status
	if len(cards) == 0 {
		annotation, ok := node.Labels[gpuListLabel]

		if !ok {
			klog.Error("gpulist label not found from node")

			return nil
		}

		cards = strings.Split(annotation, ".")
	}

	return cards
}

func getNodeGPUResourceCapacity(node *v1.Node) resourceMap {
	capacity := resourceMap{}

	for resourceName, quantity := range node.Status.Allocatable {
		if strings.HasPrefix(resourceName.String(), resourcePrefix) {
			value, _ := quantity.AsInt64()
			resName := resourceName.String()
			capacity[resName] = value
		}
	}

	return capacity
}

func getPerGPUResourceCapacity(node *v1.Node, gpuCount int) resourceMap {
	if gpuCount == 0 {
		return resourceMap{}
	}
	// fetch node resource capacity
	capacity := getNodeGPUResourceCapacity(node)

	// figure out per gpu capacity
	// (this assumes homogeneous gpus in node, alternative is to start labeling resources per gpu for the nodes)
	perGPUCapacity := capacity.newCopy()

	err := perGPUCapacity.divide(gpuCount)
	if err != nil {
		return resourceMap{}
	}

	return perGPUCapacity
}

func getPerGPUResourceRequest(containerRequest resourceMap) (resourceMap, int64) {
	perGPUResourceRequest := containerRequest.newCopy()

	numI915 := getNumI915(containerRequest)

	if numI915 > 1 {
		err := perGPUResourceRequest.divide(int(numI915))
		if err != nil {
			return perGPUResourceRequest, 0
		}
	}

	return perGPUResourceRequest, numI915
}

func getNumI915(containerRequest resourceMap) int64 {
	if numI915, ok := containerRequest[gpuPluginResource]; ok && numI915 > 0 {
		return numI915
	}

	return 0
}

// isGPUUsable returns true, if the GPU is usable.
func (m *GASExtender) isGPUUsable(gpuName string, node *v1.Node, pod *v1.Pod) bool {
	return !isGPUDisabled(gpuName, node) && m.isGPUAllowed(gpuName, pod) && !m.isGPUDenied(gpuName, pod)
}

// isGPUAllowed returns true, if the given gpuName is allowed. A GPU is considered allowed, if:
// 1) the allowlist-feature is not enabled in the first place - all gpus are allowed then
// 2) there is no annotation in the POD (nil annotations, or missing annotation) - all gpus are allowed then
// 3) there is an allowlist-annotation in the POD, and it contains the given GPU name -> true.
func (m *GASExtender) isGPUAllowed(gpuName string, pod *v1.Pod) bool {
	if !m.allowlistEnabled || pod.Annotations == nil {
		klog.V(l5).InfoS("gpu allowed", "gpuName", gpuName, "podName", pod.Name, "allowlistEnabled", m.allowlistEnabled)

		return true
	}

	allow := false

	csvAllowlist, ok := pod.Annotations[allowlistAnnotationName]
	if ok {
		allowedGPUs := createSearchMapFromStrings(strings.Split(csvAllowlist, ","))
		allow = allowedGPUs[gpuName]
	} else {
		allow = true
	}

	klog.V(l4).InfoS("gpu allow status",
		"allow", allow, "gpuName", gpuName, "podName", pod.Name, "allowlist", csvAllowlist)

	return allow
}

// isGPUDenied returns true, if the given gpuName is denied. A GPU is considered denied, if:
// 1) the denylist-feature is enabled AND
// 2) there is a denylist-annotation in the POD, and it contains the given GPU name
// Otherwise, GPU is not considered denied. Usage of allowlist at the same time, might make it in practice denied.
func (m *GASExtender) isGPUDenied(gpuName string, pod *v1.Pod) bool {
	if !m.denylistEnabled || pod.Annotations == nil {
		klog.V(l5).InfoS("gpu use not denied", "gpuName", gpuName, "podName", pod.Name, "denylistEnabled", m.denylistEnabled)

		return false
	}

	deny := false

	csvDenylist, ok := pod.Annotations[denylistAnnotationName]
	if ok {
		deniedGPUs := createSearchMapFromStrings(strings.Split(csvDenylist, ","))
		deny = deniedGPUs[gpuName]
	}

	klog.V(l4).InfoS("gpu deny status", "deny", deny, "gpuName", gpuName, "podName", pod.Name, "denylist", csvDenylist)

	return deny
}

// isGPUDisabled returns true if given gpuName should not be used based on node labels.
func isGPUDisabled(gpuName string, node *v1.Node) bool {
	// search labels that disable use of this gpu
	for label, value := range node.Labels {
		if strippedLabel, ok := labelWithoutTASNS(label); ok {
			if strings.HasPrefix(strippedLabel, gpuDisableLabelPrefix) {
				if strings.HasSuffix(label, gpuName) ||
					(value == pciGroupValue && isGPUInPCIGroup(gpuName, strippedLabel[len(gpuDisableLabelPrefix):], node)) {
					return true
				}
			}
		}
	}

	return false
}

func findNodesPreferredGPU(node *v1.Node) string {
	for label, value := range node.Labels {
		if strings.HasSuffix(label, gpuPreferenceLabel) && strings.HasPrefix(label, tasNSPrefix) {
			parts := strings.Split(label, "/")
			if len(parts) == maxLabelParts && parts[1] == gpuPreferenceLabel {
				return value
			}
		}
	}

	return ""
}

func movePreferredCardToFront(gpuNames []string, preferredCard string) {
	for i := range gpuNames {
		if gpuNames[i] == preferredCard {
			tmp := gpuNames[0]
			gpuNames[0] = preferredCard
			gpuNames[i] = tmp

			break
		}
	}
}

// The given gpuNames array must be sorted.
func arrangeGPUNamesPerResourceAvailability(nodeResourcesUsed nodeResources,
	gpuNames []string, balancedResource string) {
	keys := make([]string, 0, len(gpuNames))
	keys = append(keys, gpuNames...)

	prefixedResource := gpuPrefix + balancedResource

	// Sort keys (gpu names) in ascending order for least used resourced per the resource type
	sort.SliceStable(keys, func(i, j int) bool {
		return nodeResourcesUsed[keys[i]][prefixedResource] < nodeResourcesUsed[keys[j]][prefixedResource]
	})

	copy(gpuNames, keys)
}

func getSortedGPUNamesForNode(nodeResourcesUsed nodeResources) []string {
	gpuNames := make([]string, len(nodeResourcesUsed))
	i := 0

	for gpuName := range nodeResourcesUsed {
		gpuNames[i] = gpuName
		i++
	}

	sort.Strings(gpuNames)

	return gpuNames
}

func (m *GASExtender) createTileAnnotation(gpuName string, numCards int64, containerRequest, perGPUCapacity resourceMap,
	node *v1.Node, currentlyAllocatingTilesMap map[string][]int, preferredTiles []int) string {
	requestedTiles := containerRequest[gpuTileResource]

	requestedTilesPerGPU := requestedTiles / numCards
	if requestedTilesPerGPU == 0 {
		return ""
	}

	tileCapacityPerGPU := perGPUCapacity[gpuTileResource]
	if requestedTilesPerGPU < 0 || tileCapacityPerGPU < requestedTilesPerGPU {
		klog.Errorf("bad tile request count: %d", requestedTilesPerGPU)

		return ""
	}

	freeTiles := m.getFreeTiles(tileCapacityPerGPU, node, gpuName, currentlyAllocatingTilesMap)
	if len(freeTiles) < int(requestedTilesPerGPU) {
		klog.Errorf("not enough free tiles")

		return ""
	}

	if len(preferredTiles) > 0 {
		freeTiles = reorderPreferredTilesFirst(freeTiles, preferredTiles)
	}

	annotation := gpuName + ":"
	delimeter := ""

	for _, freeTileIndex := range freeTiles {
		annotation += delimeter + tileString + strconv.Itoa(freeTileIndex)
		currentlyAllocatingTilesMap[gpuName] = append(currentlyAllocatingTilesMap[gpuName], freeTileIndex)
		delimeter = "+"
		requestedTilesPerGPU--

		if requestedTilesPerGPU == 0 {
			break
		}
	}

	return annotation
}

func (m *GASExtender) getFreeTiles(capacityPerGPU int64, node *v1.Node,
	gpuName string, currentlyAllocatingTilesMap map[string][]int) []int {
	nTiles := iCache.GetNodeTileStatus(m.cache, node.Name)
	freeTilesMap := map[int]bool{}

	// convert capacity to bool search map with indices 0 to capacity-1
	for i := 0; i < int(capacityPerGPU); i++ {
		freeTilesMap[i] = true
	}

	// remove used tiles from map
	gpuUsedTiles := nTiles[gpuName]
	for _, usedTileIndex := range gpuUsedTiles {
		delete(freeTilesMap, usedTileIndex)
	}

	// remove currently allocating tiles from map
	currentTiles := currentlyAllocatingTilesMap[gpuName]
	for _, allocatingTileIndex := range currentTiles {
		delete(freeTilesMap, allocatingTileIndex)
	}

	tiles := []int{}
	for key := range freeTilesMap {
		tiles = append(tiles, key)
	}

	return tiles
}

func (m *GASExtender) checkGpuAvailability(gpuName string, node *v1.Node, pod *v1.Pod,
	usedGPUmap map[string]bool, gpuMap map[string]bool) bool {
	if usedGPUmap[gpuName] {
		klog.V(l4).Infof("gpu %v is already used for this container", gpuName)

		return false
	}

	if !gpuMap[gpuName] {
		klog.Warningf("node %v gpu %v has vanished", node.Name, gpuName)

		return false
	}

	// skip GPUs which are not usable and continue to next if need be
	if !m.isGPUUsable(gpuName, node, pod) {
		klog.V(l4).Infof("node %v gpu %v is not usable, skipping it", node.Name, gpuName)

		return false
	}

	return true
}

func (m *GASExtender) getCardsForContainerGPURequest(containerRequest, perGPUCapacity resourceMap,
	node *v1.Node, pod *v1.Pod,
	nodeResourcesUsed nodeResources,
	gpuMap map[string]bool) (cards []string, preferred bool, err error) {
	cards = []string{}

	if len(containerRequest) == 0 {
		return cards, preferred, nil
	}

	usedGPUmap := map[string]bool{}

	// figure out container resources per gpu
	perGPUResourceRequest, numI915 := getPerGPUResourceRequest(containerRequest)

	for gpuNum := int64(0); gpuNum < numI915; gpuNum++ {
		fitted := false
		preferredCardAtFront := false
		gpuNames := getSortedGPUNamesForNode(nodeResourcesUsed)

		if m.balancedResource != "" {
			arrangeGPUNamesPerResourceAvailability(nodeResourcesUsed, gpuNames, m.balancedResource)
		} else if preferredCard := findNodesPreferredGPU(node); preferredCard != "" {
			movePreferredCardToFront(gpuNames, preferredCard)
			preferredCardAtFront = true
		}

		for gpuIndex, gpuName := range gpuNames {
			usedResMap := nodeResourcesUsed[gpuName]
			klog.V(l4).Info("Checking gpu ", gpuName)

			if !m.checkGpuAvailability(gpuName, node, pod, usedGPUmap, gpuMap) {
				continue
			}

			if checkResourceCapacity(perGPUResourceRequest, perGPUCapacity, usedResMap) {
				err := usedResMap.addRM(perGPUResourceRequest)
				if err == nil {
					fitted = true

					if gpuIndex == 0 && preferredCardAtFront {
						preferred = true
					}

					cards = append(cards, gpuName)
					usedGPUmap[gpuName] = true
				}

				break
			}
		}

		if !fitted {
			klog.V(l4).Infof("pod %v will not fit node %v", pod.Name, node.Name)

			return nil, false, errWontFit
		}
	}

	return cards, preferred, nil
}

func createSearchMapFromStrings(list []string) map[string]bool {
	return createSearchMap(list, func(s *string) string { return *s })
}

func createSearchMapFromContainers(list []v1.Container) map[string]bool {
	return createSearchMap(list, func(container *v1.Container) string { return container.Name })
}

func createSearchMap[Key string | v1.Container](keys []Key, getKey func(*Key) string) map[string]bool {
	searchMap := make(map[string]bool, len(keys))
	for idx := range keys {
		searchMap[getKey(&keys[idx])] = true
	}

	return searchMap
}

func addEmptyResourceMaps(gpus []string, nodeResourcesUsed nodeResources) {
	for _, gpu := range gpus {
		if _, ok := nodeResourcesUsed[gpu]; !ok {
			nodeResourcesUsed[gpu] = resourceMap{}
		}
	}
}

func addUnavailableToUsedResourced(nodeResourcesUsed nodeResources, unavailableResources nodeResources) {
	for card, res := range unavailableResources {
		err := nodeResourcesUsed[card].addRM(res)
		if err != nil {
			klog.Warningf("failed to add unavailable resources to used: %w", err)
		}
	}
}

func combineSamegpuResourceRequests(indexMap map[int]bool, resourceRequests []resourceMap) (resourceMap, error) {
	combinedResources := resourceMap{}

	for index := range indexMap {
		err := combinedResources.addRM(resourceRequests[index])
		if err != nil {
			klog.Errorf("Could not sum up resources requests")

			return combinedResources, err
		}
	}

	combinedResources[gpuPluginResource] = 1

	return combinedResources, nil
}

func (m *GASExtender) getNodeForName(name string) (*v1.Node, error) {
	node, err := iCache.FetchNode(m.cache, name)

	if err != nil {
		klog.Warningf("Node %s couldn't be read or node vanished", name)

		return nil, fmt.Errorf("error reading node %s: %w", name, err)
	}

	return node, nil
}

// checkForSpaceAndRetrieveCards checks if pod fits into a node and returns the cards (gpus)
// that are assigned to each container. If pod doesn't fit or any other error triggers, error is returned.
func (m *GASExtender) checkForSpaceAndRetrieveCards(pod *v1.Pod, node *v1.Node) ([][]string, bool, error) {
	preferred := false
	containerCards := [][]string{}

	if node == nil {
		klog.Warningf("checkForSpaceAndRetrieveCards called with nil node")

		return containerCards, preferred, errWontFit
	}

	gpus := getNodeGPUList(node)
	klog.V(l4).Infof("Node %v gpu list: %v", node.Name, gpus)
	gpuCount := len(gpus)

	if gpuCount == 0 {
		klog.Warningf("Node %s GPUs have vanished", node.Name)

		return containerCards, preferred, errWontFit
	}

	perGPUCapacity := getPerGPUResourceCapacity(node, gpuCount)
	nodeResourcesUsed, err := m.readNodeResources(node.Name)

	if err != nil {
		klog.Warningf("Node %s resources couldn't be read or node vanished", node.Name)

		return containerCards, preferred, err
	}

	gpuMap := createSearchMapFromStrings(gpus)
	// add empty resourcemaps for cards which have no resources used yet
	addEmptyResourceMaps(gpus, nodeResourcesUsed)

	// create map for unavailable resources
	tilesPerGpu := perGPUCapacity[gpuTileResource]
	unavailableResources := m.createUnavailableNodeResources(node, tilesPerGpu)

	klog.V(l4).Infof("Node %v unavailable resources: %v", node.Name, unavailableResources)

	// add unavailable resources as used, unavailable resources are
	// (possible) unused resources but are marked as do-not-use externally
	// e.g. too high temperature detected on a particular resource
	addUnavailableToUsedResourced(nodeResourcesUsed, unavailableResources)

	klog.V(l4).Infof("Node %v used resources: %v", node.Name, nodeResourcesUsed)

	containerCards, preferred, err = m.checkForSpaceResourceRequests(
		perGPUCapacity, pod, node, nodeResourcesUsed, gpuMap)

	return containerCards, preferred, err
}

func (m *GASExtender) checkForSpaceResourceRequests(perGPUCapacity resourceMap, pod *v1.Pod, node *v1.Node,
	nodeResourcesUsed nodeResources, gpuMap map[string]bool) ([][]string, bool, error) {
	var err error

	var cards []string

	var samegpuCard []string

	containerCards := [][]string{}
	preferred := false

	samegpuNamesMap, err := containersRequestingSamegpu(pod)
	if err != nil {
		return containerCards, preferred, err
	}

	samegpuIndexMap, allContainerRequests := containerRequests(pod, samegpuNamesMap)

	if len(samegpuIndexMap) > 0 {
		samegpuCard, preferred, err = m.getCardForSamegpu(samegpuIndexMap, allContainerRequests,
			perGPUCapacity, node, pod, nodeResourcesUsed, gpuMap)
		if err != nil {
			return containerCards, preferred, err
		}
	}

	for i, containerRequest := range allContainerRequests {
		klog.V(l4).Infof("getting cards for container %v", i)

		if samegpuIndexMap[i] {
			klog.V(l4).Infof("found container %v in same-gpu list", i)

			containerCards = append(containerCards, samegpuCard)

			continue
		}

		cards, preferred, err = m.getCardsForContainerGPURequest(containerRequest, perGPUCapacity,
			node, pod, nodeResourcesUsed, gpuMap)

		if err != nil {
			klog.V(l4).Infof("Node %v container %v out of %v did not fit", node.Name, i+1, len(allContainerRequests))

			return containerCards, preferred, err
		}

		containerCards = append(containerCards, cards)
	}

	return containerCards, preferred, nil
}

func (m *GASExtender) getCardForSamegpu(samegpuIndexMap map[int]bool, allContainerRequests []resourceMap,
	perGPUCapacity resourceMap, node *v1.Node, pod *v1.Pod, nodeResourcesUsed nodeResources,
	gpuMap map[string]bool) ([]string, bool, error) {
	if err := sanitizeSamegpuResourcesRequest(samegpuIndexMap, allContainerRequests); err != nil {
		return []string{}, false, err
	}

	combinedResourcesRequest, fail := combineSamegpuResourceRequests(samegpuIndexMap, allContainerRequests)
	if fail != nil {
		return []string{}, false, fail
	}

	samegpuCard, preferred, err := m.getCardsForContainerGPURequest(
		combinedResourcesRequest, perGPUCapacity, node, pod, nodeResourcesUsed, gpuMap)
	if err != nil {
		klog.V(l4).Infof("Node %v same-gpu containers of pod %v did not fit", node.Name, pod.Name)

		return []string{}, false, err
	}

	bookKeepingRM := resourceMap{gpuPluginResource: int64(len(samegpuIndexMap) - 1)}

	err = nodeResourcesUsed[samegpuCard[0]].addRM(bookKeepingRM)
	if err != nil {
		klog.Errorf("Node %v could not add-up resource for bookkeeping", node.Name)

		return []string{}, false, err
	}

	klog.V(l4).Infof("Pod %v same-gpu containers fit to node %v", pod.Name, node.Name)
	klog.V(l4).Infof("Node %v used resources: %v", node.Name, nodeResourcesUsed)

	return samegpuCard, preferred, nil
}

// convertNodeCardsToAnnotations converts given container cards into card and tile
// annotation strings.
func (m *GASExtender) convertNodeCardsToAnnotations(pod *v1.Pod,
	node *v1.Node, containerCards [][]string) (annotation, tileAnnotation string) {
	gpuCount := len(getNodeGPUList(node))
	klog.V(l4).Info("Node gpu count:", gpuCount)

	perGPUCapacity := getPerGPUResourceCapacity(node, gpuCount)

	_, containerRequests := containerRequests(pod, map[string]bool{})
	containerDelimeter := ""

	if len(containerRequests) != len(containerCards) {
		klog.Errorf("sizes for containers and container cards do not match: %v vs %v",
			len(containerRequests), len(containerCards))

		return "", ""
	}

	// mark all the disabled/descheduled tiles as unusable so they wouldn't
	// get used even though they might be currently free for use
	unusableTilesMap, prefTileMap := createDisabledAndPreferredTileMapping(node.Labels)

	tilesPerGpu := perGPUCapacity[gpuTileResource]
	// it is possible to have an invalid rule which would disable a non existing
	// tile which would reduce the available resources even though it's not needed
	unusableTilesMap = sanitizeTiles(unusableTilesMap, int(tilesPerGpu))

	for i, containerRequest := range containerRequests {
		cards := containerCards[i]

		usesTiles := containerHasTiles(containerRequest)

		annotation += containerDelimeter
		tileAnnotation += containerDelimeter
		cardDelimeter := ""

		for _, card := range cards {
			annotation += cardDelimeter + card

			prefTiles := prefTileMap[card]
			if usesTiles {
				tiles := m.createTileAnnotation(card, int64(len(cards)),
					containerRequest, perGPUCapacity, node, unusableTilesMap, prefTiles)

				tileAnnotation += cardDelimeter + tiles
			}

			cardDelimeter = ","
		}

		containerDelimeter = "|"
	}

	return annotation, tileAnnotation
}

func containerHasTiles(resources resourceMap) bool {
	amount, found := resources[gpuTileResource]

	return (found && amount > 0)
}

func (m *GASExtender) createUnavailableNodeResources(node *v1.Node, tilesPerGpu int64) nodeResources {
	nodeRes := nodeResources{}

	// for now, only "supported" unavailable resource is tiles
	disabledTilesMap := createDisabledTileMapping(node.Labels)
	// it is possible to have an invalid rule which would disable a non existing
	// tile which would reduce the available resources even though it's not needed
	disabledTilesMap = sanitizeTiles(disabledTilesMap, int(tilesPerGpu))

	usedTilesStats := m.cache.nodeTileStatuses[node.Name]

	// iterate over the disabled and the used tiles
	// for the tiles that are disabled but _not_ used, increase the usage
	for card, tiles := range disabledTilesMap {
		usedTiles := usedTilesStats[card]
		resMap := resourceMap{gpuTileResource: 0}

		for _, tile := range tiles {
			if found, _ := containsInt(usedTiles, tile); !found {
				resMap[gpuTileResource]++
			}
		}

		nodeRes[card] = resMap
	}

	return nodeRes
}

// checkResourceCapacity returns true if the needed resources fit based on capacity and used resources.
func checkResourceCapacity(neededResources, capacity, used resourceMap) bool {
	for resName, resNeed := range neededResources {
		if resNeed < 0 {
			klog.Error("negative resource request")

			return false
		}

		resCapacity, ok := capacity[resName]
		if !ok || resCapacity <= 0 {
			klog.V(l4).Info(" no capacity available for ", resName)

			return false
		}

		resUsed := used[resName] // missing = 0, default value is ok

		if resUsed < 0 {
			klog.Error("negative amount of resources in use")

			return false
		}

		klog.V(l4).Info(" resource ", resName, " capacity:", strconv.FormatInt(resCapacity, base10), " used:",
			strconv.FormatInt(resUsed, base10), " need:", strconv.FormatInt(resNeed, base10))

		if resUsed+resNeed < 0 {
			klog.Error("resource request overflow error")

			return false
		}

		if resCapacity < resUsed+resNeed {
			klog.V(l4).Info(" not enough resources")

			return false
		}
	}

	klog.V(l4).Info(" there is enough resources")

	return true
}

func (m *GASExtender) retrievePod(podName, podNamespace string, uid types.UID) (*v1.Pod, error) {
	pod, err := iCache.FetchPod(m.cache, podNamespace, podName)
	if err != nil {
		klog.Warningf("Pod %s couldn't be read or pod vanished", podName)

		return nil, fmt.Errorf("could not retrieve pod %s: %w", podName, err)
	}

	if uid != pod.UID {
		klog.ErrorS(errBadUID, "bind request for pod had an invalid UID compared to cache",
			"podName", podName, "uid", uid, "cache pod.UID", pod.UID)

		return nil, errBadUID
	}

	return pod, nil
}

func (m *GASExtender) bindNode(args *extender.BindingArgs) *extender.BindingResult {
	result := extender.BindingResult{}

	pod, err := m.retrievePod(args.PodName, args.PodNamespace, args.PodUID)
	if err != nil {
		result.Error = err.Error()

		return &result
	}

	m.rwmutex.Lock()
	klog.V(l5).Infof("bind %v:%v to node %v locked", args.PodNamespace, args.PodName, args.Node)
	defer m.rwmutex.Unlock()

	resourcesAdjusted := false
	annotation, tileAnnotation := "", ""

	defer func() { // deferred errorhandler
		if err != nil {
			klog.Error("binding failed:", err.Error())
			result.Error = err.Error()

			if resourcesAdjusted {
				// Restore resources to cache. Removing resources should not fail if adding was ok.
				err = iCache.AdjustPodResourcesL(m.cache, pod, remove, annotation, tileAnnotation, args.Node)
				if err != nil {
					klog.Warning("adjust pod resources failed", err.Error())
				}
			}
		}
	}()

	// pod should always fit, but one never knows if something bad happens between filtering and binding
	node, err := m.getNodeForName(args.Node)
	if err != nil {
		return &result
	}

	cards, _, err := m.checkForSpaceAndRetrieveCards(pod, node)
	if err != nil {
		return &result
	}

	annotation, tileAnnotation = m.convertNodeCardsToAnnotations(pod, node, cards)
	if annotation == "" {
		return &result
	}

	klog.V(l3).Infof("bind %v:%v to node %v annotation %v tileAnnotation %v",
		args.PodNamespace, args.PodName, args.Node, annotation, tileAnnotation)

	err = iCache.AdjustPodResourcesL(m.cache, pod, add, annotation, tileAnnotation, args.Node)
	if err != nil {
		return &result
	}

	resourcesAdjusted = true

	err = m.annotatePodBind(annotation, tileAnnotation, pod) // annotate POD with per-container GPU selection
	if err != nil {
		return &result
	}

	binding := &v1.Binding{
		ObjectMeta: metav1.ObjectMeta{Name: args.PodName, UID: args.PodUID},
		Target:     v1.ObjectReference{Kind: "Node", Name: args.Node},
	}
	opts := metav1.CreateOptions{}
	err = m.clientset.CoreV1().Pods(args.PodNamespace).Bind(context.TODO(), binding, opts)

	return &result
}

// filterNodes takes in the arguments for the scheduler and filters nodes based on
// whether the POD resource request fits into each node.
func (m *GASExtender) filterNodes(args *extender.Args) *extender.FilterResult {
	var nodeNames []string

	var preferredNodeNames []string

	failedNodes := extender.FailedNodesMap{}
	result := extender.FilterResult{}

	if args.NodeNames == nil || len(*args.NodeNames) == 0 {
		result.Error = "No nodes to compare. " +
			"This should not happen, perhaps the extender is misconfigured with NodeCacheCapable == false."
		klog.Error(result.Error)

		return &result
	}

	m.rwmutex.Lock()
	klog.V(l5).Infof("filter %v:%v from %v locked", args.Pod.Namespace, args.Pod.Name, *args.NodeNames)
	defer m.rwmutex.Unlock()

	for _, nodeName := range *args.NodeNames {
		node, err := m.getNodeForName(nodeName)
		if err != nil {
			failedNodes[nodeName] = "Couldn't retrieve node's information"

			continue
		}

		if _, preferred, err := m.checkForSpaceAndRetrieveCards(&args.Pod, node); err == nil {
			if preferred {
				preferredNodeNames = append(preferredNodeNames, nodeName)
			} else {
				nodeNames = append(nodeNames, nodeName)
			}
		} else {
			failedNodes[nodeName] = "Not enough GPU-resources for deployment"
		}
	}

	result = extender.FilterResult{
		NodeNames:   &nodeNames,
		FailedNodes: failedNodes,
		Error:       "",
	}

	if len(preferredNodeNames) > 0 {
		result.NodeNames = &preferredNodeNames
	}

	return &result
}

// decodeRequest reads the json request into the given interface args.
// It returns an error if the request is not in the required format.
func (m *GASExtender) decodeRequest(args interface{}, r *http.Request) error {
	if r.Body == nil {
		return errEmptyBody
	}

	if klog.V(l5).Enabled() {
		requestDump, err := httputil.DumpRequest(r, true)
		if err == nil {
			klog.Infof("http-request:\n%v", string(requestDump))
		}
	}

	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&args); err != nil {
		return errDecode
	}

	err := r.Body.Close()

	if err != nil {
		err = fmt.Errorf("failed to close request body: %w", err)
	}

	return err
}

// writeResponse takes the incoming interface and writes it as a http response if valid.
func (m *GASExtender) writeResponse(w http.ResponseWriter, result interface{}) {
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(result); err != nil {
		http.Error(w, "Encode error", http.StatusBadRequest)
	}
}

// Prioritize manages all prioritize requests from the scheduler extender.
// Not implemented yet by GAS, hence response with StatusNotFound.
func (m *GASExtender) Prioritize(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
}

// Filter manages all filter requests from the scheduler. First it decodes the request,
// then it calls the filter logic and writes a response to the scheduler.
func (m *GASExtender) Filter(w http.ResponseWriter, r *http.Request) {
	klog.V(l4).Info("filter request received")

	extenderArgs := extender.Args{}
	err := m.decodeRequest(&extenderArgs, r)

	if err != nil {
		klog.Errorf("cannot decode request %v", err)
		w.WriteHeader(http.StatusNotFound)

		return
	}

	filteredNodes := m.filterNodes(&extenderArgs)
	if filteredNodes.Error != "" {
		klog.Error("filtering failed")
		w.WriteHeader(http.StatusNotFound)
	}

	m.writeResponse(w, filteredNodes)
	klog.V(l4).Info("filter function done, responded")
}

// Bind binds the pod to the node.
func (m *GASExtender) Bind(w http.ResponseWriter, r *http.Request) {
	klog.V(l4).Info("bind request received")

	extenderArgs := extender.BindingArgs{}
	err := m.decodeRequest(&extenderArgs, r)

	if err != nil {
		klog.Errorf("cannot decode request %v", err)
		w.WriteHeader(http.StatusNotFound)

		return
	}

	result := m.bindNode(&extenderArgs)
	if result.Error != "" {
		klog.Error("bind failed")
		w.WriteHeader(http.StatusNotFound)
	}

	m.writeResponse(w, result)
	klog.V(l4).Info("bind function done, responded")
}

// error handler deals with requests sent to an invalid endpoint and returns a 404.
func (m *GASExtender) errorHandler(w http.ResponseWriter, _ *http.Request) {
	klog.Error("unknown path")
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
}

func (m *GASExtender) readNodeResources(nodeName string) (nodeResources, error) {
	var err error

	resources := iCache.GetNodeResourceStatus(m.cache, nodeName)

	if resources == nil {
		err = errNotFound
	}

	return resources, err
}

// return search map of container names that should have same GPU based on samegpuAnnotationName.
// Return empty map if either there are duplicates or absent containers listed.
func containersRequestingSamegpu(pod *v1.Pod) (map[string]bool, error) {
	csvSamegpulist, ok := pod.Annotations[samegpuAnnotationName]
	if !ok {
		return map[string]bool{}, nil
	}

	samegpuContainerNames := strings.Split(csvSamegpulist, ",")

	if len(samegpuContainerNames) < samegpuMinContainers {
		klog.Errorf("malformed annotation %v: minimum %v container names required",
			samegpuAnnotationName, samegpuMinContainers)

		return map[string]bool{}, errAnnotation
	}

	samegpuMap := map[string]bool{}
	podContainerNames := createSearchMapFromContainers(pod.Spec.Containers)

	// ensure there are no duplicates and all containers are in the Pod
	for _, containerName := range samegpuContainerNames {
		if samegpuMap[containerName] {
			klog.Errorf("Malformed annotation %v: duplicate container name: %v",
				samegpuAnnotationName, containerName)

			return map[string]bool{}, errAnnotation
		}

		if !podContainerNames[containerName] {
			klog.Errorf("Malformed annotation %v: no container %v in Pod %v",
				samegpuAnnotationName, containerName, pod.Name)

			return map[string]bool{}, errAnnotation
		}

		samegpuMap[containerName] = true
	}

	klog.V(l4).Infof("Successfully parsed %v annotation in pod %v",
		samegpuAnnotationName, pod.Name)

	return samegpuMap, nil
}

func sanitizeSamegpuResourcesRequest(
	samegpuIndexMap map[int]bool, allResourceRequests []resourceMap) error {
	if len(samegpuIndexMap) == 0 {
		return nil
	}

	samegpuProhibitedResources := []string{gpuTileResource, gpuMonitoringResource}

	for idx := range samegpuIndexMap {
		request := allResourceRequests[idx]
		for _, prohibited := range samegpuProhibitedResources {
			if _, ok := request[prohibited]; ok {
				klog.Errorf(
					"Requesting %v resource is unsupported for containers listed in %v annotation",
					prohibited, samegpuAnnotationName)

				return errResConflict
			}
		}

		if getNumI915(request) != samegpuMaxI915Request {
			klog.Errorf(
				"Exactly one %v resource has to be requested for containers listed in %v annotation",
				gpuPluginResource, samegpuAnnotationName)

			return errResConflict
		}
	}

	return nil
}
