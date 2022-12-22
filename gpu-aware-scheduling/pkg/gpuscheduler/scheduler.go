// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

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
	tsAnnotationName         = "gas-ts"
	cardAnnotationName       = "gas-container-cards"
	tileAnnotationName       = "gas-container-tiles"
	allowlistAnnotationName  = "gas-allow"
	denylistAnnotationName   = "gas-deny"
	xelinkAnnotationName     = "gas-allocate-xelink"
	samegpuAnnotationName    = "gas-same-gpu"
	singleNumaAnnotationName = "gas-allocate-single-numa"
	samegpuMaxI915Request    = 1
	samegpuMinContainers     = 2
	tasNSPrefix              = "telemetry.aware.scheduling."
	gpuDisableLabelPrefix    = "gas-disable-"
	gpuPreferenceLabel       = "gas-prefer-gpu"
	tileDisableLabelPrefix   = "gas-tile-disable-"
	tileDeschedLabelPrefix   = "gas-tile-deschedule-"
	tilePrefLabelPrefix      = "gas-tile-preferred-"
	gpuPrefix                = "gpu.intel.com/"
	gpuListLabel             = gpuPrefix + "cards"
	gpuMonitoringResource    = gpuPrefix + "i915_monitoring"
	gpuNumbersLabel          = gpuPrefix + "gpu-numbers"
	gpuPluginResource        = gpuPrefix + "i915"
	gpuTileResource          = gpuPrefix + "tiles"
	numaMappingLabel         = gpuPrefix + "numa-gpu-map"
	l1                       = klog.Level(1)
	l2                       = klog.Level(2)
	l3                       = klog.Level(3)
	l4                       = klog.Level(4)
	l5                       = klog.Level(5)
	maxLabelParts            = 2
	numaSplitParts           = 2
	base10                   = 10
)

//nolint:gochecknoglobals // only mocked APIs are allowed as globals
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

//nolint:gochecknoinits // only mocked APIs are allowed in here
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

// Card represents a selected gpuName and optional xeLinkedTileIds to be used.
type Card struct {
	gpuName         string
	xeLinkedTileIds []int
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

func (m *GASExtender) annotatePodBind(ctx context.Context, annotation, tileAnnotation string, pod *v1.Pod) error {
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
		ctx, pod.GetName(), types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
	if err == nil {
		klog.V(l2).Infof("Annotated pod %v with annotation %v", pod.GetName(), annotation)
	} else {
		klog.Errorf("Pod %s annotating failed. Err %v", pod.GetName(), err.Error())
		err = fmt.Errorf("pod %s annotation failed: %w", pod.GetName(), err)
	}

	return err
}

func getCardNameSlice(gpuNumbers string) []string {
	indexes := strings.Split(gpuNumbers, ".")
	cards := make([]string, 0, len(indexes))

	for _, index := range indexes {
		cards = append(cards, "card"+index)
	}

	return cards
}

func getNodeGPUList(node *v1.Node) []string {
	if node == nil || node.Labels == nil {
		klog.Error("No labels in node")

		return nil
	}

	var cards = []string{}

	if gpuNumbersValue := concatenateSplitLabel(node, gpuNumbersLabel); gpuNumbersValue != "" {
		cards = getCardNameSlice(gpuNumbersValue)
	}

	// Deprecated, remove after intel device plugins release-0.23 drops to unsupported status
	if len(cards) == 0 {
		label, ok := node.Labels[gpuListLabel]

		if !ok {
			klog.Error("gpulist label not found from node")

			return nil
		}

		cards = strings.Split(label, ".")
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
// 2) there is no annotation in the Pod (nil annotations, or missing annotation) - all gpus are allowed then
// 3) there is an allowlist-annotation in the Pod, and it contains the given GPU name -> true.
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

func (m *GASExtender) createTileAnnotation(card Card, numCards int64, containerRequest, perGPUCapacity resourceMap,
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

	// currently only supported xeLinked configuration is 1 connection from each allocated GPU
	if len(card.xeLinkedTileIds) == 1 && requestedTilesPerGPU == 1 {
		return card.gpuName + ":gt" + strconv.Itoa(card.xeLinkedTileIds[0])
	}

	freeTiles := m.getFreeTiles(tileCapacityPerGPU, node, card.gpuName, currentlyAllocatingTilesMap)
	if len(freeTiles) < int(requestedTilesPerGPU) {
		klog.Errorf("not enough free tiles")

		return ""
	}

	if len(preferredTiles) > 0 {
		freeTiles = reorderPreferredTilesFirst(freeTiles, preferredTiles)
	}

	annotation := card.gpuName + ":"
	delimeter := ""

	for _, freeTileIndex := range freeTiles {
		annotation += delimeter + tileString + strconv.Itoa(freeTileIndex)
		currentlyAllocatingTilesMap[card.gpuName] = append(currentlyAllocatingTilesMap[card.gpuName], freeTileIndex)
		delimeter = "+"
		requestedTilesPerGPU--

		if requestedTilesPerGPU == 0 {
			break
		}
	}

	return annotation
}

func (m *GASExtender) getFreeTiles(tileCapacityPerGPU int64, node *v1.Node,
	gpuName string, currentlyAllocatingTilesMap map[string][]int) []int {
	nTiles := iCache.GetNodeTileStatus(m.cache, node.Name)
	freeTilesMap := map[int]bool{}

	// convert capacity to bool search map with indices 0 to capacity-1
	for i := 0; i < int(tileCapacityPerGPU); i++ {
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
		return false
	}

	// skip GPUs which are not usable and continue to next if need be
	if !m.isGPUUsable(gpuName, node, pod) {
		klog.V(l4).Infof("node %v gpu %v is not usable, skipping it", node.Name, gpuName)

		return false
	}

	return true
}

/*
findXeLinkedGPUPair utility function finds a suitable xe-linked gpu pair. It needs all the possible info.
nodeTilesAllocating is the tiles which are marked for potential use by previous containers of the pod.

for an xe-link to be usable,
  - check if the GPU has resources and is available
  - loop GPU free xe-linked tiles
  - check if the linked GPU has resources and is available
  - check if the linked GPU linked tile is free
    if all the above are valid, the GPU pair can be allocated.
*/
func (m *GASExtender) findXeLinkedGPUPair(gpuNames []string,
	node *v1.Node, pod *v1.Pod,
	nodeResourcesUsed nodeResources,
	availableTiles, nodeTilesAllocating nodeTiles,
	perGPUResourceRequest, perGPUCapacity resourceMap,
	gpuMap, usedGPUmap map[string]bool) (cards []Card, err error) {
	cards = []Card{}
	err = errWontFit
	found := false

	for _, gpuName := range gpuNames {
		usedResMap := nodeResourcesUsed[gpuName]
		klog.V(l4).Info("Checking gpu ", gpuName)

		if !m.checkGpuAvailability(gpuName, node, pod, usedGPUmap, gpuMap) {
			continue
		}

		if !checkResourceCapacity(perGPUResourceRequest, perGPUCapacity, usedResMap) {
			continue
		}

		for _, tileIndex := range availableTiles[gpuName] {
			linkedGpuName, linkedTileID := getXeLinkedGPUInfo(gpuName, tileIndex, node)
			klog.V(l4).Infof("Checking linked gpu %v tile id %v", gpuName, linkedTileID)

			if !m.checkGpuAvailability(linkedGpuName, node, pod, usedGPUmap, gpuMap) {
				continue
			}

			linkedGpuUsedResMap := nodeResourcesUsed[linkedGpuName]
			if contains, _ := containsInt(availableTiles[linkedGpuName], linkedTileID); contains &&
				checkResourceCapacity(perGPUResourceRequest, perGPUCapacity, linkedGpuUsedResMap) {
				err = usedResMap.addRM(perGPUResourceRequest)
				if err != nil {
					return []Card{}, err
				}

				err = linkedGpuUsedResMap.addRM(perGPUResourceRequest)
				if err != nil {
					err2 := usedResMap.subtractRM(perGPUResourceRequest)
					klog.Errorf("resource addition failure: %v, subtraction result: %v", err.Error(), err2)

					return []Card{}, err
				}

				klog.V(l4).Infof("gpu %v tile id %v and linked gpu %v tile id %v fits",
					gpuName, tileIndex, linkedGpuName, linkedTileID)

				found = true

				cards = append(cards, []Card{{gpuName: gpuName, xeLinkedTileIds: []int{tileIndex}},
					{gpuName: linkedGpuName, xeLinkedTileIds: []int{linkedTileID}}}...)
				usedGPUmap[gpuName] = true
				usedGPUmap[linkedGpuName] = true

				break // xe-linked tile search loop
			}
		}

		if found {
			for _, card := range cards {
				nodeTilesAllocating[card.gpuName] = append(nodeTilesAllocating[card.gpuName], card.xeLinkedTileIds...)
			}

			break // double-gpu search loop
		}
	}

	return cards, err
}

func (m *GASExtender) getXELinkedCardsForContainerGPURequest(containerRequest, perGPUCapacity resourceMap,
	node *v1.Node, pod *v1.Pod,
	nodeResourcesUsed nodeResources,
	nodeTilesAllocating nodeTiles,
	gpuMap map[string]bool) (cards []Card, preferred bool, err error) {
	cards = []Card{}

	if len(containerRequest) == 0 {
		return cards, preferred, nil
	}

	usedGPUmap := map[string]bool{}

	// figure out container resources per gpu
	perGPUResourceRequest, numI915 := getPerGPUResourceRequest(containerRequest)

	if numI915%2 != 0 {
		klog.Errorf("xe-linked allocations must have an even numbered gpu resource request")

		return []Card{}, preferred, errBadArgs
	}

	preferredCard := ""

	for gpuNum := int64(0); gpuNum < numI915; gpuNum += 2 {
		gpuNames := getSortedGPUNamesForNode(nodeResourcesUsed)

		if m.balancedResource != "" {
			arrangeGPUNamesPerResourceAvailability(nodeResourcesUsed, gpuNames, m.balancedResource)
		} else if preferredCard = findNodesPreferredGPU(node); preferredCard != "" {
			movePreferredCardToFront(gpuNames, preferredCard)
		}

		availableTiles := m.createAvailableXeLinkedTilesStat(node,
			int(perGPUCapacity[gpuTileResource]), gpuNames, nodeTilesAllocating)

		cardPair, err := m.findXeLinkedGPUPair(gpuNames, node, pod, nodeResourcesUsed, availableTiles, nodeTilesAllocating,
			perGPUResourceRequest, perGPUCapacity, gpuMap, usedGPUmap)

		if err != nil {
			return []Card{}, preferred, err
		}

		cards = append(cards, cardPair...)
	}

	preferred = (len(cards) > 0 && cards[0].gpuName == preferredCard)

	return cards, preferred, nil
}

func (m *GASExtender) getCardsForContainerGPURequest(containerRequest, perGPUCapacity resourceMap,
	node *v1.Node, pod *v1.Pod,
	nodeResourcesUsed nodeResources,
	gpuMap map[string]bool) (cards []Card, preferred bool, err error) {
	cards = []Card{}

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

					cards = append(cards, Card{gpuName: gpuName})
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

func addUnavailableToUsedResources(nodeResourcesUsed nodeResources, unavailableResources nodeResources) {
	for card, res := range unavailableResources {
		if usedResources := nodeResourcesUsed[card]; usedResources != nil {
			err := usedResources.addRM(res)
			if err != nil {
				klog.Warningf("failed to add unavailable resources to used: %w", err)
			}
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

func checkPod(pod *v1.Pod) error {
	if pod == nil {
		return errBadArgs
	}

	_, xeLinkAnnotationPresent := pod.Annotations[xelinkAnnotationName]
	_, sameGpuAnnotationPresent := pod.Annotations[samegpuAnnotationName]

	if xeLinkAnnotationPresent && sameGpuAnnotationPresent {
		klog.Errorf("annotations %v and %v can't be used at the same time", xelinkAnnotationName, samegpuAnnotationName)

		return errBadArgs
	}

	return nil
}

// createGPUMaps returns gpu search maps for each numa node or an all gpu search map
// in case single numa allocations are not asked for.
func createGPUMaps(pod *v1.Pod, node *v1.Node, allGPUs []string) []map[string]bool {
	maps := []map[string]bool{}

	if pod.Annotations == nil || node.Labels == nil {
		return []map[string]bool{createSearchMapFromStrings(allGPUs)}
	}

	_, singleNumaRequested := pod.Annotations[singleNumaAnnotationName]
	gpuNumaInformation, nodeHasNumaInfo := node.Labels[numaMappingLabel]

	if singleNumaRequested && nodeHasNumaInfo {
		numaGroups := strings.Split(gpuNumaInformation, "_")

		for _, numaGroup := range numaGroups {
			numaGroupSplit := strings.Split(numaGroup, "-")

			if len(numaGroupSplit) != numaSplitParts {
				klog.Error("node %v bad numa group in label %s", node.Name, gpuNumaInformation)

				return []map[string]bool{}
			}

			gpuNumbers := numaGroupSplit[1]

			cardNames := getCardNameSlice(gpuNumbers)

			maps = append(maps, createSearchMapFromStrings(cardNames))
		}
	} else {
		return []map[string]bool{createSearchMapFromStrings(allGPUs)}
	}

	return maps
}

// checkForSpaceAndRetrieveCards checks if pod fits into a node and returns the cards (gpus)
// that are assigned to each container. If pod doesn't fit or any other error triggers, error is returned.
func (m *GASExtender) checkForSpaceAndRetrieveCards(pod *v1.Pod, node *v1.Node) ([][]Card, bool, error) {
	preferred := false
	containerCards := [][]Card{}

	if node == nil {
		klog.Warningf("checkForSpaceAndRetrieveCards called with nil node")

		return containerCards, preferred, errWontFit
	}

	if err := checkPod(pod); err != nil {
		return [][]Card{}, false, err
	}

	gpus := getNodeGPUList(node)
	klog.V(l4).Infof("Node %v gpu list: %v", node.Name, gpus)
	gpuCount := len(gpus)

	if gpuCount == 0 {
		klog.Warningf("Node %s GPUs have vanished", node.Name)

		return [][]Card{}, false, errWontFit
	}

	perGPUCapacity := getPerGPUResourceCapacity(node, gpuCount)
	nodeResourcesUsed, err := m.readNodeResources(node.Name)

	if err != nil {
		klog.Warningf("Node %s resources couldn't be read or node vanished", node.Name)

		return [][]Card{}, false, err
	}

	// form maps of gpus to search, default is all in one map, otherwise per numa node
	gpuMaps := createGPUMaps(pod, node, gpus)
	// add empty resourcemaps for cards which have no resources used yet
	addEmptyResourceMaps(gpus, nodeResourcesUsed)

	// create map for unavailable resources
	tilesPerGpu := perGPUCapacity[gpuTileResource]
	unavailableResources := m.createUnavailableNodeResources(node, tilesPerGpu)

	klog.V(l4).Infof("Node %v unavailable resources: %v", node.Name, unavailableResources)

	// add unavailable resources as used, unavailable resources are
	// (possible) unused resources but are marked as do-not-use externally
	// e.g. too high temperature detected on a particular resource
	addUnavailableToUsedResources(nodeResourcesUsed, unavailableResources)

	klog.V(l4).Infof("Node %v used resources: %v", node.Name, nodeResourcesUsed)

	containerCards, preferred, err = m.checkForSpaceResourceRequests(
		perGPUCapacity, pod, node, nodeResourcesUsed, gpuMaps)

	return containerCards, preferred, err
}

func (m *GASExtender) checkForSpaceResourceRequests(perGPUCapacity resourceMap, pod *v1.Pod, node *v1.Node,
	nodeResourcesUsed nodeResources, gpuMaps []map[string]bool) ([][]Card, bool, error) {
	var err error

	var cards []Card

	var samegpuCard []Card

	containerCards := [][]Card{}
	preferred := false

	samegpuNamesMap, err := containersRequestingSamegpu(pod)
	if err != nil {
		return containerCards, preferred, err
	}

	samegpuIndexMap, allContainerRequests := containerRequests(pod, samegpuNamesMap)

	if len(samegpuIndexMap) > 0 {
		samegpuCard, preferred, err = m.getCardForSamegpu(samegpuIndexMap, allContainerRequests,
			perGPUCapacity, node, pod, nodeResourcesUsed, gpuMaps[0])
		if err != nil {
			return containerCards, preferred, err
		}
	}

	nodeTilesAllocating := nodeTiles{}

	for i, containerRequest := range allContainerRequests {
		if samegpuIndexMap[i] {
			klog.V(l4).Infof("found container %v in same-gpu list", i)

			containerCards = append(containerCards, samegpuCard)

			continue
		}

		// loop through gpu maps per numa node, or all gpus if single numa allocation is not requested
		for _, gpuMap := range gpuMaps {
			klog.V(l4).Infof("getting cards for container %v", i)

			if _, ok := pod.Annotations[xelinkAnnotationName]; ok {
				cards, preferred, err = m.getXELinkedCardsForContainerGPURequest(containerRequest, perGPUCapacity,
					node, pod, nodeResourcesUsed, nodeTilesAllocating, gpuMap)
			} else {
				cards, preferred, err = m.getCardsForContainerGPURequest(containerRequest, perGPUCapacity,
					node, pod, nodeResourcesUsed, gpuMap)
			}

			if err == nil {
				containerCards = append(containerCards, cards)

				break
			}
		}

		if err != nil {
			klog.V(l4).Infof("Node %v container %v out of %v did not fit", node.Name, i+1, len(allContainerRequests))

			return containerCards, preferred, err
		}
	}

	return containerCards, preferred, nil
}

func (m *GASExtender) getCardForSamegpu(samegpuIndexMap map[int]bool, allContainerRequests []resourceMap,
	perGPUCapacity resourceMap, node *v1.Node, pod *v1.Pod, nodeResourcesUsed nodeResources,
	gpuMap map[string]bool) ([]Card, bool, error) {
	gpuMapCopy := deepCopySimpleMap(gpuMap)

	if err := sanitizeSamegpuResourcesRequest(samegpuIndexMap, allContainerRequests); err != nil {
		return []Card{}, false, err
	}

	combinedResourcesRequest, fail := combineSamegpuResourceRequests(samegpuIndexMap, allContainerRequests)
	if fail != nil {
		return []Card{}, false, fail
	}

	// combinedResourcesRequest ends up with a hard-coded 1 i915 resource only, so we prune the gpuMapCopy, if needed
	reallyNeededI915Resources := len(samegpuIndexMap)
	for gpuName, gpuUsedResources := range nodeResourcesUsed {
		if perGPUCapacity[gpuPluginResource]-gpuUsedResources[gpuPluginResource] < int64(reallyNeededI915Resources) {
			delete(gpuMapCopy, gpuName)
		}
	}

	samegpuCard, preferred, err := m.getCardsForContainerGPURequest(
		combinedResourcesRequest, perGPUCapacity, node, pod, nodeResourcesUsed, gpuMapCopy)
	if err != nil {
		klog.V(l4).Infof("Node %v same-gpu containers of pod %v did not fit", node.Name, pod.Name)

		return []Card{}, false, err
	}

	bookKeepingRM := resourceMap{gpuPluginResource: int64(len(samegpuIndexMap) - 1)}

	err = nodeResourcesUsed[samegpuCard[0].gpuName].addRM(bookKeepingRM)
	if err != nil {
		klog.Errorf("Node %v could not add-up resource for bookkeeping", node.Name)

		return []Card{}, false, err
	}

	klog.V(l4).Infof("Pod %v same-gpu containers fit to node %v", pod.Name, node.Name)
	klog.V(l4).Infof("Node %v used resources: %v", node.Name, nodeResourcesUsed)

	return samegpuCard, preferred, nil
}

// convertNodeCardsToAnnotations converts given container cards into card and tile
// annotation strings.
func (m *GASExtender) convertNodeCardsToAnnotations(pod *v1.Pod,
	node *v1.Node, containerCards [][]Card) (annotation, tileAnnotation string) {
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
			annotation += cardDelimeter + card.gpuName

			prefTiles := prefTileMap[card.gpuName]
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

// createUnavailableTilesStat returns disabled+descheduled+used+unusable (e.g. currently allocating)
// tiles. May have duplicate indices.
func (m *GASExtender) createUnavailableTilesStat(node *v1.Node, tilesPerGpu int, unusableTiles nodeTiles) nodeTiles {
	disabledTilesMap := createDisabledTileMapping(node.Labels)
	// it is possible to have an invalid rule which would disable a non existing
	// tile which would reduce the available resources even though it's not needed
	disabledTilesMap = sanitizeTiles(disabledTilesMap, tilesPerGpu)

	usedTilesStats := iCache.GetNodeTileStatus(m.cache, node.Name)
	combineMappings(disabledTilesMap, usedTilesStats)
	// node tile status doesn't include currently allocating tiles yet
	combineMappings(unusableTiles, usedTilesStats)

	return usedTilesStats
}

// createAvailableXeLinkedTilesStat returns the available xe-linked tiles of the node.
// You can provide a map of currently allocating tiles to be excluded from the available.
func (m *GASExtender) createAvailableXeLinkedTilesStat(node *v1.Node,
	tileCapacityPerGPU int,
	gpuNames []string,
	nodeTilesAllocating nodeTiles) nodeTiles {
	availableTiles := nodeTiles{}

	unavailableTiles := m.createUnavailableTilesStat(node, tileCapacityPerGPU, nodeTilesAllocating)

	for _, gpuName := range gpuNames {
		gpuAvailableTiles := getXeLinkedTiles(gpuName, node)

		for _, index := range unavailableTiles[gpuName] {
			delete(gpuAvailableTiles, index)
		}

		available := make([]int, len(gpuAvailableTiles))

		i := 0

		for k := range gpuAvailableTiles {
			available[i] = k
			i++
		}

		availableTiles[gpuName] = available
	}

	return availableTiles
}

func (m *GASExtender) createUnavailableNodeResources(node *v1.Node, tileCapacityPerGPU int64) nodeResources {
	nodeRes := nodeResources{}

	// for now, only "supported" unavailable resource is tiles
	disabledTilesMap := createDisabledTileMapping(node.Labels)
	// it is possible to have an invalid rule which would disable a non existing
	// tile which would reduce the available resources even though it's not needed
	disabledTilesMap = sanitizeTiles(disabledTilesMap, int(tileCapacityPerGPU))

	usedTilesStats := iCache.GetNodeTileStatus(m.cache, node.Name)

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

func (m *GASExtender) bindNode(ctx context.Context, args *extender.BindingArgs) *extender.BindingResult {
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

	err = m.annotatePodBind(ctx, annotation, tileAnnotation, pod) // annotate POD with per-container GPU selection
	if err != nil {
		return &result
	}

	binding := &v1.Binding{
		ObjectMeta: metav1.ObjectMeta{Name: args.PodName, UID: args.PodUID},
		Target:     v1.ObjectReference{Kind: "Node", Name: args.Node},
	}
	opts := metav1.CreateOptions{}
	err = m.clientset.CoreV1().Pods(args.PodNamespace).Bind(ctx, binding, opts)

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
			failedNodes[nodeName] = "Not enough GPU-resources for deployment: " + err.Error()
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

	result := m.bindNode(r.Context(), &extenderArgs)
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
