// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// inspired by https://github.com/AliyunContainerService/gpushare-scheduler-extender

package gpuscheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	add                      = true
	remove                   = false
	workerWaitTime           = time.Millisecond * 100
	informerInterval         = 0
	gpuDescheduleLabelPrefix = "gas-deschedule-pods-"
	podDescheduleString      = "gpu.aware.scheduling~1deschedule-pod"
	pciGroupValue            = "PCI_GROUP"
	tileString               = "gt"
	expectedGpuSplitCount    = 2
)

//nolint:gochecknoglobals // only mocked APIs are allowed as globals
var (
	internCacheAPI InternalCacheAPI
)

// Errors.
var (
	errUnknownAction = errors.New("unknown action")
	errHandling      = errors.New("error handling pod")
	errBadArgs       = errors.New("bad args")
)

//nolint:gochecknoinits // only mocked APIs are allowed in here
func init() {
	internCacheAPI = &internalCacheAPI{}
}

type patchValue struct {
	Value interface{} `json:"value"`
	Op    string      `json:"op"`
	Path  string      `json:"path"`
}

// Cache : basically all things cached, including the resulting resource usage statuses per card
// Nodes cache is needed for incoming scheduling request so that not all node objects need to be
// sent for every scheduled pod. Also for detecting new labels in nodes.
// Pods cache is needed during the scheduling request so that not all pods need to be read from
// all nodes for every scheduled pod.
// The cache could be accessed from multiple goroutines and therefore needs concurrency protection,
// which is achieved with a mutex.
type Cache struct {
	clientset             kubernetes.Interface
	sharedInformerFactory informers.SharedInformerFactory
	nodeLister            corev1.NodeLister
	podWorkQueue          workqueue.RateLimitingInterface
	nodeWorkQueue         workqueue.RateLimitingInterface
	podLister             corev1.PodLister
	annotatedPods         map[string]string
	nodeStatuses          map[string]nodeResources
	nodeTileStatuses      map[string]nodeTiles
	previousDeschedCards  map[string][]string /* node -> list of cards */
	previousDeschedTiles  map[string][]string /* node -> list of card+tile combos "x.y" */
	podDeschedStatuses    map[string]bool
	rwmutex               sync.RWMutex
}

// Node resources = a map of resourceMaps accessed by node gpu names.
type nodeResources map[string]resourceMap

// Node tiles = map to slice of indices of used tiles (gpu name -> []int).
type nodeTiles map[string][]int

const /*pod action*/ (
	podUpdated = iota
	podAdded
	podDeleted
	podCompleted
)

type podWorkQueueItem struct {
	pod            *v1.Pod
	name           string
	ns             string
	annotation     string
	tileAnnotation string
	action         int
}

const /* node action*/ (
	nodeUpdated = iota
	nodeAdded
	nodeDeleted
)

type nodeWorkQueueItem struct {
	node     *v1.Node
	nodeName string
	action   int
}

func (c *Cache) createFilteringPodResourceHandler() *cache.FilteringResourceEventHandler {
	return &cache.FilteringResourceEventHandler{
		FilterFunc: c.podFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addPodToCache,
			UpdateFunc: c.updatePodInCache,
			DeleteFunc: c.deletePodFromCache,
		},
	}
}

func (c *Cache) createFilteringNodeResourceHandler() *cache.FilteringResourceEventHandler {
	return &cache.FilteringResourceEventHandler{
		FilterFunc: c.nodeFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addNodeToCache,
			UpdateFunc: c.updateNodeInCache,
			DeleteFunc: c.deleteNodeFromCache,
		},
	}
}

// NewCache returns a new Cache object.
func NewCache(client kubernetes.Interface) *Cache {
	if client == nil {
		klog.Error("Can't create cache with nil clientset")

		return nil
	}

	sharedInformerFactory := informers.NewSharedInformerFactory(client, informerInterval)
	nodeInformer := sharedInformerFactory.Core().V1().Nodes()
	nodeLister := nodeInformer.Lister()
	podInformer := sharedInformerFactory.Core().V1().Pods()
	podLister := podInformer.Lister()
	stopChannel := signalHandler()

	klog.V(logL1).Info("starting shared informer factory (cache)")

	go sharedInformerFactory.Start(stopChannel)

	syncOk := internCacheAPI.WaitForCacheSync(stopChannel, nodeInformer.Informer().HasSynced)
	if syncOk {
		klog.V(logL2).Info("node cache created and synced successfully")
	} else {
		klog.Error("Couldn't sync clientgo cache for nodes")

		return nil
	}

	syncOk = internCacheAPI.WaitForCacheSync(stopChannel, podInformer.Informer().HasSynced)
	if syncOk {
		klog.V(logL2).Info("POD cache created and synced successfully")
	} else {
		klog.Error("Couldn't sync clientgo cache for PODs")

		return nil
	}

	cache := Cache{
		clientset:             client,
		sharedInformerFactory: sharedInformerFactory,
		nodeLister:            nodeLister,
		podWorkQueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "podWorkQueue"),
		nodeWorkQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "nodeWorkQueue"),
		podLister:             podLister,
		annotatedPods:         make(map[string]string),
		nodeStatuses:          make(map[string]nodeResources),
		nodeTileStatuses:      make(map[string]nodeTiles),
		previousDeschedCards:  make(map[string][]string),
		previousDeschedTiles:  make(map[string][]string),
		podDeschedStatuses:    make(map[string]bool),
		rwmutex:               sync.RWMutex{},
	}

	_, err := podInformer.Informer().AddEventHandler(cache.createFilteringPodResourceHandler())
	_, err2 := nodeInformer.Informer().AddEventHandler(cache.createFilteringNodeResourceHandler())

	if err != nil || err2 != nil {
		klog.Errorf("informer event handler init failure (%v, %v)", err, err2)

		return nil
	}

	go func() { cache.startPodWork(stopChannel) }()
	go func() { cache.startNodeWork(stopChannel) }()

	return &cache
}

func (c *Cache) podFilter(obj interface{}) bool {
	var pod *v1.Pod

	var ok1 bool

	switch t := obj.(type) {
	case *v1.Pod:
		pod, _ = obj.(*v1.Pod)
	case cache.DeletedFinalStateUnknown:
		pod, ok1 = t.Obj.(*v1.Pod)

		if !ok1 {
			return false
		}
	default:
		return false
	}

	return hasGPUResources(pod)
}

func (c *Cache) nodeFilter(obj interface{}) bool {
	var node *v1.Node

	var ok1 bool

	switch t := obj.(type) {
	case *v1.Node:
		node, _ = obj.(*v1.Node)
	case cache.DeletedFinalStateUnknown:
		node, ok1 = t.Obj.(*v1.Node)

		if !ok1 {
			return false
		}
	default:
		return false
	}

	return hasGPUCapacity(node)
}

// This must be called with rwmutex unlocked
// set add=true to add, false to remove resources.
func (c *Cache) adjustPodResourcesL(pod *v1.Pod, adj bool, annotation, tileAnnotation, nodeName string) error {
	klog.V(logL4).Infof("adjustPodResourcesL %v %v", nodeName, pod.Name)
	c.rwmutex.Lock()
	klog.V(logL5).Infof("adjustPodResourcesL %v %v locked", nodeName, pod.Name)
	defer c.rwmutex.Unlock()

	err := c.adjustPodResources(pod, adj, annotation, tileAnnotation, nodeName)

	return err
}

// newCopyNodeStatus creates a new copy of node resources for given node name.
// This must be called with the rwmutex at least read-locked.
func (c *Cache) newCopyNodeStatus(nodeName string) nodeResources {
	nodeRes := nodeResources{}

	if srcNodeRes, ok := c.nodeStatuses[nodeName]; ok {
		for cardName := range srcNodeRes {
			nodeRes[cardName] = srcNodeRes[cardName].newCopy()
		}
	}

	return nodeRes
}

// checkPodResourceAdjustment goes through all the containers and checks for errors in
// the node resource-map arithmetics (like integer overflows). If any fail, this returns an error.
// This must be called with the rwmutex at least read-locked.
// set adj=true to add, false to remove resources.
func (c *Cache) checkPodResourceAdjustment(containerRequests []resourceMap,
	nodeName string, containerCards []string, adj bool,
) error {
	if len(containerRequests) != len(containerCards) || nodeName == "" {
		klog.Errorf("bad args, node %v pod creqs %v ccards %v", nodeName, containerRequests, containerCards)

		return errBadArgs
	}

	return c.checkPodResourceAdjustmentImpl(containerRequests, nodeName, containerCards, adj)
}

func (c *Cache) checkPodResourceAdjustmentImpl(containerRequests []resourceMap,
	nodeName string, containerCards []string, adj bool,
) error {
	numContainers := len(containerRequests)
	nodeRes := c.newCopyNodeStatus(nodeName)

	var err error

	for index := 0; index < numContainers; index++ {
		// get slice of card names from the CSV list of container nr i
		cardNames := strings.Split(containerCards[index], ",")
		numCards := len(cardNames)

		if numCards == 0 || len(containerCards[index]) == 0 {
			continue
		}

		request := containerRequests[index].newCopy()

		err = request.divide(numCards)
		if err != nil {
			return err
		}

		for _, cardName := range cardNames {
			_, ok := nodeRes[cardName]
			if !ok {
				nodeRes[cardName] = resourceMap{}
			}

			if adj { // add
				err = nodeRes[cardName].addRM(request)
			} else {
				err = nodeRes[cardName].subtractRM(request)
			}

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func getTileIndices(tileNames []string) []int {
	tileIndices := []int{}

	for _, tileName := range tileNames {
		if strings.HasPrefix(tileName, tileString) {
			index, err := strconv.Atoi(tileName[len(tileString):])
			if err == nil && index >= 0 {
				tileIndices = append(tileIndices, index)
			}
		}
	}

	return tileIndices
}

// This must be called with rwmutex locked
// set adj=true to add, false to remove resources.
func (c *Cache) adjustTiles(adj bool, nodeName, tileAnnotation string) {
	tileUsage, ok := c.nodeTileStatuses[nodeName]
	if !ok {
		c.nodeTileStatuses[nodeName] = nodeTiles{}
		tileUsage = c.nodeTileStatuses[nodeName]
	}

	containerSplit := strings.Split(tileAnnotation, "|")

	numContainers := len(containerSplit)
	for i := 0; i < numContainers; i++ {
		if len(containerSplit[i]) == 0 {
			continue
		}

		gpuSplit := strings.Split(containerSplit[i], ",")
		for _, gpuString := range gpuSplit {
			gpuParts := strings.Split(gpuString, ":")
			if len(gpuParts) == expectedGpuSplitCount {
				gpuName := gpuParts[0]
				tiles := strings.Split(gpuParts[1], "+")
				usedTilesMap := map[int]bool{}

				oldUsedTiles := tileUsage[gpuName]
				for _, tileIndex := range oldUsedTiles {
					usedTilesMap[tileIndex] = true
				}

				newTileIndices := getTileIndices(tiles)
				for _, tileIndex := range newTileIndices {
					if adj {
						usedTilesMap[tileIndex] = true
					} else {
						delete(usedTilesMap, tileIndex)
					}
				}

				finalUsedTilesSlice := []int{}
				for usedTile := range usedTilesMap {
					finalUsedTilesSlice = append(finalUsedTilesSlice, usedTile)
				}

				tileUsage[gpuName] = finalUsedTilesSlice
			}
		}
	}
}

func (c *Cache) blindAdjustResources(adj bool, srcResMap, dstResMap resourceMap) {
	if adj { // add
		_ = dstResMap.addRM(srcResMap)
	} else {
		_ = dstResMap.subtractRM(srcResMap)
	}
}

// This must be called with rwmutex locked
// set adj=true to add, false to remove resources.
func (c *Cache) adjustPodResources(pod *v1.Pod, adj bool, annotation, tileAnnotation, nodeName string) error {
	// get slice of resource maps, one map per container
	_, containerRequests := containerRequests(pod, map[string]bool{})

	// get slice of card name lists, one CSV list per container
	containerCards := strings.Split(annotation, "|")

	// we need to be atomic, either all succeed or none succeed, so check first
	err := c.checkPodResourceAdjustment(containerRequests, nodeName, containerCards, adj)
	if err != nil {
		return err
	}

	// now that we have checked, error checks are omitted below
	numContainers := len(containerRequests)
	for index := 0; index < numContainers; index++ {
		// get slice of card names from the CSV list of container nr i
		cardNames := strings.Split(containerCards[index], ",")
		numCards := len(cardNames)

		if numCards == 0 || len(containerCards[index]) == 0 {
			continue
		}

		err = containerRequests[index].divide(numCards)
		if err != nil {
			return err
		}

		if _, ok := c.nodeStatuses[nodeName]; !ok {
			c.nodeStatuses[nodeName] = nodeResources{}
		}

		for _, cardName := range cardNames {
			_, ok := c.nodeStatuses[nodeName][cardName]
			if !ok {
				c.nodeStatuses[nodeName][cardName] = resourceMap{}
			}

			c.blindAdjustResources(adj, containerRequests[index], c.nodeStatuses[nodeName][cardName])
		}
	}

	c.adjustTiles(adj, nodeName, tileAnnotation)

	if adj { // add
		c.annotatedPods[getKey(pod)] = annotation
	} else {
		delete(c.annotatedPods, getKey(pod))
	}

	c.printNodeStatus(nodeName)

	return nil
}

func signalHandler() <-chan struct{} {
	stopChan := make(chan struct{})
	//nolint:gomnd
	signalChan := make(chan os.Signal, 2)
	signal.Notify(signalChan, []os.Signal{os.Interrupt, syscall.SIGTERM}...)

	go func() {
		<-signalChan
		close(stopChan)
		<-signalChan
		os.Exit(1)
	}()

	return stopChan
}

// calculateCardsFromDescheduleLabels returns an array of cards which are currently
// indicated for descheduling.
func calculateCardsFromDescheduleLabels(node *v1.Node) []string {
	cards := []string{}

	for label, value := range node.Labels {
		if !strings.HasPrefix(label, tasNSPrefix) {
			continue
		}

		parts := strings.Split(label, "/")
		if len(parts) == 2 &&
			strings.HasPrefix(parts[1], gpuDescheduleLabelPrefix) {
			card := parts[1][len(gpuDescheduleLabelPrefix):]

			if found := containsString(cards, card); !found {
				cards = append(cards, card)
			}

			if value == pciGroupValue {
				cards = addPCIGroupGPUs(node, card, cards)
			}
		}
	}

	return cards
}

func calculateTilesFromDescheduleLabels(node *v1.Node) []string {
	deschedTiles := []string{}

	_, des, _ := createTileMapping(node.Labels)

	for card, tiles := range des {
		cardIndex := card[len("card"):]

		for _, tile := range tiles {
			tileStr := strconv.Itoa(tile)
			deschedTiles = append(deschedTiles, cardIndex+"."+tileStr)
		}
	}

	return deschedTiles
}

func (c *Cache) addNodeToCache(nodeObj interface{}) {
	node, ok := nodeObj.(*v1.Node)
	if !ok {
		klog.Warningf("cannot convert to *v1.Node: %v", nodeObj)

		return
	}

	item := nodeWorkQueueItem{
		node:     node,
		nodeName: node.Name,
		action:   nodeAdded,
	}
	c.nodeWorkQueue.Add(item)
}

func (c *Cache) updateNodeInCache(_, newNodeObj interface{}) {
	node, ok := newNodeObj.(*v1.Node)
	if !ok {
		klog.Warningf("cannot convert to *v1.Node: %v", newNodeObj)

		return
	}

	item := nodeWorkQueueItem{
		node:     node,
		nodeName: node.Name,
		action:   nodeUpdated,
	}
	c.nodeWorkQueue.Add(item)
}

func (c *Cache) deleteNodeFromCache(nodeObj interface{}) {
	var node *v1.Node
	switch aType := nodeObj.(type) {
	case *v1.Node:
		node = aType
	case cache.DeletedFinalStateUnknown:
		var ok bool
		node, ok = aType.Obj.(*v1.Node)

		if !ok {
			klog.Warningf("cannot convert to *v1.Node: %v", aType.Obj)

			return
		}
	default:
		klog.Warningf("cannot convert to *v1.Node: %v", aType)

		return
	}

	item := nodeWorkQueueItem{
		node:     node,
		nodeName: node.Name,
		action:   nodeDeleted,
	}
	c.nodeWorkQueue.Add(item)
}

func (c *Cache) addPodToCache(podObj interface{}) {
	pod, ok := podObj.(*v1.Pod)
	if !ok {
		klog.Warningf("cannot convert to *v1.Pod: %v", podObj)

		return
	}

	// if POD does not have the necessary annotation, working on it is futile, then update must be waited for
	annotation, ok2 := pod.Annotations[cardAnnotationName]
	if !ok2 {
		return
	}

	tileAnnotation := pod.Annotations[tileAnnotationName] // default value "" is ok, if not found

	item := podWorkQueueItem{
		ns:             pod.Namespace,
		name:           pod.Name,
		annotation:     annotation,
		tileAnnotation: tileAnnotation,
		pod:            pod,
		action:         podAdded,
	}
	c.podWorkQueue.Add(item)
}

func (c *Cache) updatePodInCache(_, newPodObj interface{}) {
	newPod, ok := newPodObj.(*v1.Pod)
	if !ok {
		klog.Warningf("conversion of newObj -> pod failed: %v", newPodObj)

		return
	}

	// if POD does not have the necessary annotation, can't work on it yet
	annotation, ok2 := newPod.Annotations[cardAnnotationName]
	if !ok2 {
		return
	}

	tileAnnotation := newPod.Annotations[tileAnnotationName] // default value "" is ok, if not found

	item := podWorkQueueItem{
		name:           newPod.Name,
		ns:             newPod.Namespace,
		annotation:     annotation,
		tileAnnotation: tileAnnotation,
		pod:            newPod,
		action:         podUpdated,
	}

	// Change action to completed if pod is completed
	if isCompletedPod(newPod) {
		item.action = podCompleted
	}

	c.podWorkQueue.Add(item)
}

func (c *Cache) deletePodFromCache(podObj interface{}) {
	klog.V(logL4).Infof("deletePodFromCache")
	c.rwmutex.RLock() // reads c.annotatedPods
	klog.V(logL5).Infof("deletePodFromCache locked")
	defer c.rwmutex.RUnlock()

	var pod *v1.Pod
	switch aType := podObj.(type) {
	case *v1.Pod:
		pod = aType
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = aType.Obj.(*v1.Pod)

		if !ok {
			klog.Warningf("cannot convert to *v1.Pod: %v", aType.Obj)

			return
		}
	default:
		klog.Warningf("cannot convert to *v1.Pod: %v", aType)

		return
	}

	key := getKey(pod)
	_, annotatedPod := c.annotatedPods[key]

	klog.V(logL4).Infof("delete pod %s in ns %s annotated:%v", pod.Name, pod.Namespace, annotatedPod)

	if !annotatedPod {
		return
	}

	item := podWorkQueueItem{
		ns:             pod.Namespace,
		name:           pod.Name,
		pod:            pod,
		action:         podDeleted,
		annotation:     "",
		tileAnnotation: "",
	}
	c.podWorkQueue.Add(item)
}

func (c *Cache) startNodeWork(stopChannel <-chan struct{}) {
	defer c.nodeWorkQueue.ShutDown()
	defer runtime.HandleCrash()

	klog.V(logL2).Info("starting node worker")

	// block calling goroutine
	wait.Until(c.nodeWorkerRun, workerWaitTime, stopChannel)

	klog.V(logL2).Info("node worker shutting down")
}

// This steals the calling goroutine and blocks doing work.
func (c *Cache) startPodWork(stopChannel <-chan struct{}) {
	defer c.podWorkQueue.ShutDown()
	defer runtime.HandleCrash()

	klog.V(logL2).Info("starting pod worker")

	// block calling goroutine
	wait.Until(c.podWorkerRun, workerWaitTime, stopChannel)

	klog.V(logL2).Info("pod worker shutting down")
}

func (c *Cache) podWorkerRun() {
	for c.podWork() {
	}
}

func (c *Cache) nodeWorkerRun() {
	for c.nodeWork() {
	}
}

func (c *Cache) nodeWork() bool {
	klog.V(logL5).Info("node worker started")

	itemI, quit := c.nodeWorkQueue.Get()

	if quit {
		klog.V(logL2).Info("node worker quitting")

		return false
	}

	defer c.nodeWorkQueue.Done(itemI)
	defer klog.V(logL5).Info("node worker ended work")

	item, ok := itemI.(nodeWorkQueueItem)

	if !ok {
		klog.Error("type check failure")

		return false
	}

	err := c.handleNode(item)

	if err == nil {
		c.nodeWorkQueue.Forget(itemI)

		return true
	}

	klog.Errorf("error handling node %v: %v", item.nodeName, err)
	runtime.HandleError(errHandling)

	return true
}

func (c *Cache) podWork() bool {
	klog.V(logL5).Info("pod worker started")

	itemI, quit := c.podWorkQueue.Get()

	if quit {
		klog.V(logL2).Info("pod worker quitting")

		return false
	}

	defer c.podWorkQueue.Done(itemI)
	defer klog.V(logL5).Info("pod worker ended work")

	item, ok := itemI.(podWorkQueueItem)

	if !ok {
		klog.Error("type check failure")

		return false
	}

	forget, err := c.handlePod(item)

	if err == nil {
		if forget {
			c.podWorkQueue.Forget(itemI)
		}

		return true
	}

	klog.Errorf("error handling pod %v ns %v: %v", item.name, item.ns, err)
	runtime.HandleError(errHandling)

	return true
}

func getKey(pod *v1.Pod) string {
	return pod.Namespace + "&" + pod.Name
}

// this fetches a node by a name.
func (c *Cache) fetchNode(nodeName string) (*v1.Node, error) {
	node, err := c.nodeLister.Get(nodeName)
	if err != nil {
		return nil, fmt.Errorf("node fetch error: %w", err)
	}

	return node, nil
}

func (c *Cache) fetchPod(ns, name string) (*v1.Pod, error) {
	nsLister := c.podLister.Pods(ns)

	pod, err := nsLister.Get(name)
	if err != nil {
		return nil, fmt.Errorf("pod fetch error: %w", err)
	}

	return pod.DeepCopy(), nil
}

// getNodeTileStatus returns a copy of current tile status for a node.
func (c *Cache) getNodeTileStatus(nodeName string) nodeTiles {
	klog.V(logL4).Infof("getNodeTileStatus %v", nodeName)
	c.rwmutex.RLock()
	klog.V(logL5).Infof("getNodeTileStatus %v locked", nodeName)
	defer c.rwmutex.RUnlock()

	dstNodeTiles := nodeTiles{}

	// deep copy
	for gpuName, tiles := range c.nodeTileStatuses[nodeName] {
		dstNodeTiles[gpuName] = append(dstNodeTiles[gpuName], tiles...)
	}

	return dstNodeTiles
}

// getNodeResourceStatus returns a copy of current resource status for a node (map of per card resource maps).
func (c *Cache) getNodeResourceStatus(nodeName string) nodeResources {
	klog.V(logL4).Infof("getNodeResourceStatus %v", nodeName)
	c.rwmutex.RLock()
	klog.V(logL5).Infof("getNodeResourceStatus %v locked", nodeName)
	defer c.rwmutex.RUnlock()

	dstNodeResources := nodeResources{}

	// deep copy
	for cardName, rm := range c.nodeStatuses[nodeName] {
		dstNodeResources[cardName] = resourceMap{}
		for key, value := range rm {
			dstNodeResources[cardName][key] = value
		}
	}

	return dstNodeResources
}

func allPodGPUs(pod *v1.Pod) map[string]bool {
	gpus := map[string]bool{}

	if annotation, ok := pod.Annotations[cardAnnotationName]; ok {
		lists := strings.Split(annotation, "|")
		for _, list := range lists {
			gpuList := strings.Split(list, ",")
			for _, gpuName := range gpuList {
				if strings.HasPrefix(gpuName, "card") {
					gpus[gpuName] = true
				}
			}
		}
	}

	return gpus
}

func allPodTiles(pod *v1.Pod) map[string]bool {
	tiles := map[string]bool{}

	if annotation, ok := pod.Annotations[tileAnnotationName]; ok {
		return convertPodTileAnnotationToCardTileMap(annotation)
	}

	return tiles
}

func isDeschedulingNeededCards(pod *v1.Pod, cards []string) bool {
	podGPUs := allPodGPUs(pod)

	for _, card := range cards {
		if _, ok := podGPUs[card]; ok {
			return true
		}
	}

	return false
}

func isDeschedulingNeededTiles(pod *v1.Pod, tiles []string) bool {
	podTiles := allPodTiles(pod)

	for _, card := range tiles {
		if _, ok := podTiles[card]; ok {
			return true
		}
	}

	return false
}

// handlePodDescheduleLabeling adds or removes labels for which the descheduler then
// deschedules pods from the node.
func (c *Cache) handlePodDescheduleLabeling(deschedule bool, pod *v1.Pod) error {
	payload := []patchValue{}

	if deschedule {
		payload = append(payload, patchValue{
			Op:    "add",
			Path:  "/metadata/labels/" + podDescheduleString,
			Value: "gpu",
		})
	} else {
		payload = append(payload, patchValue{
			Op:    "remove",
			Path:  "/metadata/labels/" + podDescheduleString,
			Value: "",
		})
	}

	payloadBytes, merr := json.Marshal(payload)

	if merr != nil {
		klog.Errorf("Json marshal failed for Pod: %s: %s.", pod.GetName(), merr.Error())

		return fmt.Errorf("marshaling failed: %w", merr)
	}

	_, err := c.clientset.CoreV1().Pods(pod.GetNamespace()).Patch(
		context.TODO(), pod.GetName(), types.JSONPatchType, payloadBytes, metav1.PatchOptions{
			TypeMeta: metav1.TypeMeta{
				Kind:       "",
				APIVersion: "",
			},
			DryRun:          []string{},
			Force:           new(bool),
			FieldManager:    "",
			FieldValidation: "",
		})
	if err == nil {
		klog.V(logL4).Infof("Pod %s labeled successfully.", pod.GetName())

		return nil
	}

	klog.Errorf("Pod %s labeling failed: %s", pod.GetName(), err.Error())

	return fmt.Errorf("pod label failed: %w", err)
}

func createListOptions(selector string) *metav1.ListOptions {
	return &metav1.ListOptions{
		TypeMeta:             metav1.TypeMeta{Kind: "", APIVersion: ""},
		LabelSelector:        "",
		FieldSelector:        selector,
		Watch:                false,
		AllowWatchBookmarks:  false,
		ResourceVersion:      "",
		ResourceVersionMatch: "",
		TimeoutSeconds:       new(int64),
		Limit:                0,
		Continue:             "",
		SendInitialEvents:    nil,
	}
}

func (c *Cache) handleNodeUpdated(item nodeWorkQueueItem) error {
	// add and remove related labels
	// calculate set of cards that trigger descheduling and compare it to the previous
	// set of cards. then if it has changed, move to study pods/containers for changes.
	descheduledCards := calculateCardsFromDescheduleLabels(item.node)
	descheduledTiles := calculateTilesFromDescheduleLabels(item.node)

	sort.Strings(descheduledCards)
	sort.Strings(descheduledTiles)

	prevDescheduleCards := c.previousDeschedCards[item.nodeName]
	prevDescheduleTiles := c.previousDeschedTiles[item.nodeName]

	if reflect.DeepEqual(descheduledCards, prevDescheduleCards) &&
		reflect.DeepEqual(descheduledTiles, prevDescheduleTiles) {
		return nil
	}

	selector, err := fields.ParseSelector("spec.nodeName=" + item.nodeName +
		",status.phase=" + string(v1.PodRunning))
	if err != nil {
		klog.Error(err.Error())

		return fmt.Errorf("error with fetching object: %w", err)
	}

	runningPodList, err := c.clientset.CoreV1().Pods(v1.NamespaceAll).List(context.TODO(),
		*createListOptions(selector.String()))
	if err != nil {
		klog.Error(err.Error())

		return fmt.Errorf("error with listing pods: %w", err)
	}

	for index := range runningPodList.Items {
		podName := runningPodList.Items[index].Name
		needDeschedule := (isDeschedulingNeededCards(&runningPodList.Items[index], descheduledCards) ||
			isDeschedulingNeededTiles(&runningPodList.Items[index], descheduledTiles))

		// change pod's descheduling label based on the need (if it doesn't exist vs. if it does)
		if needDeschedule != c.podDeschedStatuses[podName] {
			if err := c.handlePodDescheduleLabeling(needDeschedule, &runningPodList.Items[index]); err != nil {
				return err
			}

			c.podDeschedStatuses[podName] = needDeschedule
		}
	}

	// update previous descheduling cards
	c.previousDeschedCards[item.nodeName] = descheduledCards
	c.previousDeschedTiles[item.nodeName] = descheduledTiles

	return nil
}

func (c *Cache) handleNode(item nodeWorkQueueItem) error {
	klog.V(logL4).Infof("handleNode %s", item.nodeName)

	c.rwmutex.Lock() // reads and writes c. fields
	klog.V(logL5).Infof("handleNode %v locked", item.nodeName)
	defer c.rwmutex.Unlock()

	var err error

	switch item.action {
	case nodeAdded:
		fallthrough
	case nodeUpdated:
		err = c.handleNodeUpdated(item)
	case nodeDeleted:
		delete(c.previousDeschedCards, item.nodeName)
		delete(c.previousDeschedTiles, item.nodeName)
	}

	return err
}

func (c *Cache) handlePod(item podWorkQueueItem) (bool, error) {
	var err error

	klog.V(logL4).Infof("handlePod %s in ns %s", item.name, item.ns)

	c.rwmutex.Lock() // adjusts podresources
	klog.V(logL5).Infof("handlePod %v locked", item.name)
	defer c.rwmutex.Unlock()

	msg := ""
	key := getKey(item.pod)

	switch item.action {
	case podCompleted:
		msg += "podCompleted -> "

		fallthrough
	case podDeleted:
		annotation, annotatedPod := c.annotatedPods[key]
		if annotatedPod {
			msg += "podDeleted, key:" + key + " annotation:" + annotation
			err = c.adjustPodResources(item.pod, remove, item.annotation, item.tileAnnotation, item.pod.Spec.NodeName)
		} else {
			msg += "podDeleted, key:" + key + " annotation already gone"
		}

		delete(c.podDeschedStatuses, item.name)
	case podAdded:
		msg += "podAdded -> "

		c.podDeschedStatuses[item.name] = false

		fallthrough
	case podUpdated:
		_, alreadyAnnotated := c.annotatedPods[key]
		if alreadyAnnotated {
			msg += "podUpdated, key:" + key + " annotation already present"
		} else {
			msg += "podUpdated, key:" + key + " annotation:" + item.annotation
			err = c.adjustPodResources(item.pod, add, item.annotation, item.tileAnnotation, item.pod.Spec.NodeName)
		}
	default:
		msg = "unknown action"
		err = errUnknownAction
	}

	klog.V(logL4).Infof(msg)

	c.printNodeStatus(item.pod.Spec.NodeName)

	return true, err
}

func (c *Cache) printNodeStatus(nodeName string) {
	if klog.V(logL4).Enabled() {
		klog.Info(nodeName, ":")
		resources, ok := c.nodeStatuses[nodeName]

		if ok {
			for key, value := range resources {
				klog.Info("    ", key, ":", value)
			}
		}

		tileUsage, ok2 := c.nodeTileStatuses[nodeName]

		if ok2 {
			for key, value := range tileUsage {
				klog.Info("    ", key, " used tiles:", value)
			}
		}
	}
}
