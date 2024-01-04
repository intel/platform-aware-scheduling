// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package gpuscheduler

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	// resourcePrefix is the intel resource prefix.
	resourcePrefix    = "gpu.intel.com/"
	pciGroupLabel     = "gpu.intel.com/pci-groups"
	xeLinksLabel      = "gpu.intel.com/xe-links"
	regexCardTile     = "^card([0-9]+)_gt([0-9]+)$"
	regexXeLink       = "^([0-9]+)\\.([0-9]+)-([0-9]+)\\.([0-9]+)$"
	digitBase         = 10
	desiredIntBits    = 16
	regexDesiredCount = 3
	regexXeLinkCount  = 5
	labelControlChar  = "Z"
)

// Globals for compiled regexps. No other global types here!
var (
	cardTileReg = regexp.MustCompile(regexCardTile)
	xeLinkReg   = regexp.MustCompile(regexXeLink)
)

type (
	DisabledTilesMap    map[string][]int
	DescheduledTilesMap map[string][]int
	PreferredTilesMap   map[string][]int
)

// Return all resources requests and samegpuSearchmap indicating which resourceRequests
// should be counted together. samegpuSearchmap is same length as samegpuContainerNames arg,
// Key is index of allResource item, value is true if container was listed in same-gpu annotation.
func containerRequests(pod *v1.Pod, samegpuContainerNames map[string]bool) (
	map[int]bool, []resourceMap,
) {
	samegpuSearchMap := map[int]bool{}
	allResources := []resourceMap{}

	for idx, container := range pod.Spec.Containers {
		resMap := resourceMap{}

		for name, quantity := range container.Resources.Requests {
			resourceName := name.String()
			if strings.HasPrefix(resourceName, gpuPrefix) {
				value, _ := quantity.AsInt64()
				resMap[resourceName] = value
			}
		}

		if samegpuContainerNames[container.Name] {
			samegpuSearchMap[idx] = true
		}

		allResources = append(allResources, resMap)
	}

	return samegpuSearchMap, allResources
}

// addPCIGroupGPUs processes the given card and if it is requested to be handled as groups, the
// card's group is added to the cards slice.
func addPCIGroupGPUs(node *v1.Node, card string, cards []string) []string {
	pciGroupGPUNums := getPCIGroup(node, card)
	for _, gpuNum := range pciGroupGPUNums {
		groupedCard := cardPrefix + gpuNum
		if found := containsString(cards, groupedCard); !found {
			cards = append(cards, groupedCard)
		}
	}

	return cards
}

func extractCardAndTile(cardTileCombo string) (string, int, error) {
	card := ""
	tile := -1

	values := cardTileReg.FindStringSubmatch(cardTileCombo)
	if len(values) != regexDesiredCount {
		return card, tile, errExtractFail
	}

	card = cardPrefix + values[1]
	tile, _ = strconv.Atoi(values[2])

	return card, tile, nil
}

func createTileMapping(labels map[string]string) (
	DisabledTilesMap, DescheduledTilesMap, PreferredTilesMap,
) {
	disabled := DisabledTilesMap{}
	descheduled := DescheduledTilesMap{}
	preferred := PreferredTilesMap{}

	for label, value := range labels {
		stripped, ok := labelWithoutTASNS(label)
		if !ok {
			continue
		}

		switch {
		case strings.HasPrefix(stripped, tileDisableLabelPrefix):
			{
				cardTileCombo := strings.TrimPrefix(stripped, tileDisableLabelPrefix)

				card, tile, err := extractCardAndTile(cardTileCombo)
				if err == nil {
					disabled[card] = append(disabled[card], tile)
				}
			}
		case strings.HasPrefix(stripped, tileDeschedLabelPrefix):
			{
				cardTileCombo := strings.TrimPrefix(stripped, tileDeschedLabelPrefix)

				card, tile, err := extractCardAndTile(cardTileCombo)
				if err == nil {
					descheduled[card] = append(descheduled[card], tile)
				}
			}
		case strings.HasPrefix(stripped, tilePrefLabelPrefix):
			{
				cardWithoutTile := strings.TrimPrefix(stripped, tilePrefLabelPrefix)
				cardWithTile := cardWithoutTile + "_" + value

				card, tile, err := extractCardAndTile(cardWithTile)
				if err == nil {
					preferred[card] = append(preferred[card], tile)
				}
			}
		default:
			continue
		}
	}

	return disabled, descheduled, preferred
}

func combineMappings(source map[string][]int, dest map[string][]int) {
	for card, tiles := range source {
		dest[card] = append(dest[card], tiles...)
	}
}

// creates a card to tile-index map which are in either state "disabled" or "descheduled".
func createDisabledTileMapping(labels map[string]string) map[string][]int {
	dis, des, _ := createTileMapping(labels)

	combineMappings(des, dis)

	return dis
}

// creates two card to tile-index maps where first is disabled and second is preferred mapping.
func createDisabledAndPreferredTileMapping(labels map[string]string) (
	DisabledTilesMap, PreferredTilesMap,
) {
	dis, des, pref := createTileMapping(labels)

	combineMappings(des, dis)

	return dis, pref
}

func sanitizeTiles(tilesMap DisabledTilesMap, tilesPerGpu int) DisabledTilesMap {
	sanitized := DisabledTilesMap{}

	for card, tiles := range tilesMap {
		stiles := []int{}

		for _, tile := range tiles {
			if tile < tilesPerGpu {
				stiles = append(stiles, tile)
			} else {
				klog.Warningf("skipping a non existing tile: %s, tile %d", card, tile)
			}
		}

		sanitized[card] = stiles
	}

	return sanitized
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
		if gpuName == cardPrefix+gpuNum {
			return true
		}
	}

	return false
}

// concatenateSplitLabel returns the given label value and concatenates any
// additional values for label names with a running number postfix starting with "2".
// Subsequent values should start with the control character 'Z'.
func concatenateSplitLabel(node *v1.Node, labelName string) string {
	postFix := 2
	value := node.Labels[labelName]

	for continuingLabelValue, ok1 := node.Labels[labelName+strconv.Itoa(postFix)]; ok1; {
		if !strings.HasPrefix(continuingLabelValue, labelControlChar) {
			klog.Warningf("concatenated chuck has invalid prefix: %s", continuingLabelValue[:len(labelControlChar)])

			return ""
		}

		value += continuingLabelValue[len(labelControlChar):]

		postFix++
		continuingLabelValue, ok1 = node.Labels[labelName+strconv.Itoa(postFix)]
	}

	return value
}

// getPCIGroup returns the pci group as slice, for the given gpu name.
func getPCIGroup(node *v1.Node, gpuName string) []string {
	if pciGroups := concatenateSplitLabel(node, pciGroupLabel); pciGroups != "" {
		slicedGroups := strings.Split(pciGroups, "_")
		for _, group := range slicedGroups {
			gpuNums := strings.Split(group, ".")
			for _, gpuNum := range gpuNums {
				if cardPrefix+gpuNum == gpuName {
					return gpuNums
				}
			}
		}
	}

	return []string{}
}

func deepCopySimpleMap[k comparable, v int | bool | string](simpleMap map[k]v) map[k]v {
	mapCopy := map[k]v{}

	for key, value := range simpleMap {
		mapCopy[key] = value
	}

	return mapCopy
}

func hasGPUCapacity(node *v1.Node) bool {
	if node == nil {
		return false
	}

	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		if quantity, ok := node.Status.Capacity[v1.ResourceName(pluginResourceName)]; ok {
			numGPU, _ := quantity.AsInt64()
			if numGPU > 0 {
				return true
			}
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

func containsInt(slice []int, value int) (bool, int) {
	for index, v := range slice {
		if v == value {
			return true, index
		}
	}

	return false, -1
}

func containsString(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}

	return false
}

func reorderPreferredTilesFirst(tiles []int, preferred []int) []int {
	indexNow := 0

	for _, pref := range preferred {
		if found, index := containsInt(tiles, pref); found {
			if index > indexNow {
				old := tiles[indexNow]
				tiles[indexNow] = pref
				tiles[index] = old
			}

			indexNow++
		}
	}

	return tiles
}

func getXeLinkedTiles(gpuName string, node *v1.Node) map[int]bool {
	xeLinkedTiles := map[int]bool{}

	xeLinkLabelValue := concatenateSplitLabel(node, xeLinksLabel)
	lZeroDeviceID := gpuNameToLZeroDeviceID(gpuName, node)

	if lZeroDeviceID == -1 || xeLinkLabelValue == "" {
		return xeLinkedTiles
	}

	xeLinkSlice := strings.Split(xeLinkLabelValue, "_")

	for _, linkPair := range xeLinkSlice {
		submatches := xeLinkReg.FindStringSubmatch(linkPair)
		if len(submatches) != regexXeLinkCount {
			klog.Errorf("Malformed Xe Link label part: %v", linkPair)

			return xeLinkedTiles
		}

		if submatches[1] == strconv.Itoa(lZeroDeviceID) {
			tileNumber, err := strconv.Atoi(submatches[2])
			if err == nil {
				xeLinkedTiles[tileNumber] = true
			}
		} else if submatches[3] == strconv.Itoa(lZeroDeviceID) {
			tileNumber, err := strconv.Atoi(submatches[4])
			if err == nil {
				xeLinkedTiles[tileNumber] = true
			}
		}
	}

	return xeLinkedTiles
}

type linkInfo struct {
	lZeroDeviceID          int
	lZeroSubdeviceID       int
	linkedLZeroDeviceID    int
	linkedLZeroSubdeviceID int
}

func parseXeLink(link string) (linkInfo, error) {
	lInfo := linkInfo{
		lZeroDeviceID:          0,
		lZeroSubdeviceID:       0,
		linkedLZeroDeviceID:    0,
		linkedLZeroSubdeviceID: 0,
	}

	submatches := xeLinkReg.FindStringSubmatch(link)

	if len(submatches) != regexXeLinkCount {
		return lInfo, errBadArgs
	}

	identifiers := [4]int{}

	for i := 1; i < regexXeLinkCount; i++ {
		var err error
		identifiers[i-1], err = strconv.Atoi(submatches[i])

		if err != nil {
			return lInfo, errors.Wrap(err, "bad xe-link string")
		}
	}

	lInfo.lZeroDeviceID = identifiers[0]
	lInfo.lZeroSubdeviceID = identifiers[1]
	lInfo.linkedLZeroDeviceID = identifiers[2]
	lInfo.linkedLZeroSubdeviceID = identifiers[3]

	return lInfo, nil
}

func getXeLinkedGPUInfo(gpuName string, tileIndex int, node *v1.Node) (string, int) {
	xeLinkLabelValue := concatenateSplitLabel(node, xeLinksLabel)
	lZeroDeviceID := gpuNameToLZeroDeviceID(gpuName, node)

	if lZeroDeviceID == -1 || xeLinkLabelValue == "" {
		return "", -1
	}

	xeLinkSlice := strings.Split(xeLinkLabelValue, "_")

	for _, linkPair := range xeLinkSlice {
		lInfo, err := parseXeLink(linkPair)
		if err != nil {
			return "", -1
		}

		if lInfo.lZeroDeviceID == lZeroDeviceID && lInfo.lZeroSubdeviceID == tileIndex {
			return lZeroDeviceIDToGpuName(lInfo.linkedLZeroDeviceID, node), lInfo.linkedLZeroSubdeviceID
		}
	}

	return "", -1
}

func gpuNameToLZeroDeviceID(gpuName string, node *v1.Node) int {
	gpuNumSlice := numSortedGpuNums(node)

	for i, gpuNum := range gpuNumSlice {
		if cardPrefix+gpuNum == gpuName {
			return i
		}
	}

	return -1
}

func lZeroDeviceIDToGpuName(lZeroID int, node *v1.Node) string {
	gpuNumSlice := numSortedGpuNums(node)

	if lZeroID >= len(gpuNumSlice) || lZeroID < 0 || gpuNumSlice[lZeroID] == "" {
		return ""
	}

	return cardPrefix + gpuNumSlice[lZeroID]
}

func numSortedGpuNums(node *v1.Node) []string {
	gpuNums := concatenateSplitLabel(node, gpuNumbersLabel)

	gpuNumSlice := strings.Split(gpuNums, ".")

	failed := false

	// sort gpuNumSlice numerically
	sort.Slice(gpuNumSlice, func(i, j int) bool {
		iVal, errI := strconv.Atoi(gpuNumSlice[i])
		jVal, errJ := strconv.Atoi(gpuNumSlice[j])

		if errI != nil || errJ != nil {
			failed = true
			klog.Errorf("malformed %v label value %q, strconv results %v and %v", gpuNumbersLabel, gpuNums, errI, errJ)

			return false
		}

		return iVal < jVal
	})

	if failed {
		return nil
	}

	return gpuNumSlice
}

func convertPodTileAnnotationToCardTileMap(podTileAnnotation string) map[string]bool {
	cardTileIndices := make(map[string]bool)

	containerCardList := strings.Split(podTileAnnotation, "|")

	for _, contAnnotation := range containerCardList {
		cardTileList := strings.Split(contAnnotation, ",")

		for _, cardTileCombos := range cardTileList {
			cardTileSplit := strings.Split(cardTileCombos, ":")
			if len(cardTileSplit) != maxLabelParts {
				continue
			}

			// extract card index by moving forward in slice
			cardIndexStr := cardTileSplit[0][len(cardPrefix):]

			_, err := strconv.ParseInt(cardIndexStr, digitBase, desiredIntBits)
			if err != nil {
				continue
			}

			tiles := strings.Split(cardTileSplit[1], "+")
			for _, tile := range tiles {
				tileNoStr := strings.TrimPrefix(tile, "gt")

				_, err := strconv.ParseInt(tileNoStr, digitBase, desiredIntBits)
				if err == nil {
					cardTileIndices[cardIndexStr+"."+tileNoStr] = true
				}
			}
		}
	}

	return cardTileIndices
}
