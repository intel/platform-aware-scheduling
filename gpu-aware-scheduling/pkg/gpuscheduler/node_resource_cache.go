// inspired by https://github.com/AliyunContainerService/gpushare-scheduler-extender

package gpuscheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
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
	informerInterval         = time.Second * 30
	gpuDescheduleLabelPrefix = "gas-deschedule-pods-"
	podDescheduleString      = "gpu.aware.scheduling~1deschedule-pod"
	pciGroupValue            = "PCI_GROUP"
)

//nolint: gochecknoglobals // only mocked APIs are allowed as globals
var (
	internCacheAPI InternalCacheAPI
)

// Errors.
var (
	errUnknownAction = errors.New("unknown action")
	errHandling      = errors.New("error handling pod")
	errBadArgs       = errors.New("bad args")
)

//nolint: gochecknoinits // only mocked APIs are allowed in here
func init() {
	internCacheAPI = &internalCacheAPI{}
}

type patchValue struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
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
	rwmutex               sync.RWMutex
	nodeStatuses          map[string]nodeResources
	knownNodeLabels       map[string]map[string]string /* nodename -> label name -> label value */
}

// Node resources = a map of resourceMaps accessed by node card names.
type nodeResources map[string]resourceMap

const /*pod action*/ (
	podUpdated = iota
	podAdded
	podDeleted
	podCompleted
)

type podWorkQueueItem struct {
	name       string
	ns         string
	annotation string
	action     int
	pod        *v1.Pod
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

	klog.V(L1).Info("starting shared informer factory (cache)")

	go sharedInformerFactory.Start(stopChannel)

	syncOk := internCacheAPI.WaitForCacheSync(stopChannel, nodeInformer.Informer().HasSynced)
	if syncOk {
		klog.V(L2).Info("node cache created and synced successfully")
	} else {
		klog.Error("Couldn't sync clientgo cache for nodes")

		return nil
	}

	syncOk = internCacheAPI.WaitForCacheSync(stopChannel, podInformer.Informer().HasSynced)
	if syncOk {
		klog.V(L2).Info("POD cache created and synced successfully")
	} else {
		klog.Error("Couldn't sync clientgo cache for PODs")

		return nil
	}

	c := Cache{
		clientset:             client,
		sharedInformerFactory: sharedInformerFactory,
		nodeLister:            nodeLister,
		podWorkQueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "podWorkQueue"),
		nodeWorkQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "nodeWorkQueue"),
		podLister:             podLister,
		annotatedPods:         make(map[string]string),
		knownNodeLabels:       make(map[string]map[string]string),
		nodeStatuses:          make(map[string]nodeResources),
	}

	podInformer.Informer().AddEventHandler(c.createFilteringPodResourceHandler())
	nodeInformer.Informer().AddEventHandler(c.createFilteringNodeResourceHandler())

	go func() { c.startPodWork(stopChannel) }()
	go func() { c.startNodeWork(stopChannel) }()

	return &c
}

func (c *Cache) podFilter(obj interface{}) bool {
	var pod *v1.Pod

	var ok bool

	switch t := obj.(type) {
	case *v1.Pod:
		pod, _ = obj.(*v1.Pod)
	case cache.DeletedFinalStateUnknown:
		pod, ok = t.Obj.(*v1.Pod)

		if !ok {
			return false
		}
	default:
		return false
	}

	return hasGPUResources(pod)
}

func (c *Cache) nodeFilter(obj interface{}) bool {
	var node *v1.Node

	var ok bool

	switch t := obj.(type) {
	case *v1.Node:
		node, _ = obj.(*v1.Node)
	case cache.DeletedFinalStateUnknown:
		node, ok = t.Obj.(*v1.Node)

		if !ok {
			return false
		}
	default:
		return false
	}

	return hasGPUCapacity(node)
}

// This must be called with rwmutex unlocked
// set add=true to add, false to remove resources.
func (c *Cache) adjustPodResourcesL(pod *v1.Pod, adj bool, annotation, nodeName string) error {
	klog.V(L4).Infof("adjustPodResourcesL %v %v", nodeName, pod.Name)
	c.rwmutex.Lock()
	klog.V(L5).Infof("adjustPodResourcesL %v %v locked", nodeName, pod.Name)
	defer c.rwmutex.Unlock()

	err := c.adjustPodResources(pod, adj, annotation, nodeName)

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
func (c *Cache) checkPodResourceAdjustment(containerRequests []resourceMap,
	nodeName string, containerCards []string, adj bool) error {
	if len(containerRequests) != len(containerCards) || nodeName == "" {
		klog.Errorf("bad args, node %v pod creqs %v ccards %v", nodeName, containerRequests, containerCards)

		return errBadArgs
	}

	numContainers := len(containerRequests)
	nodeRes := c.newCopyNodeStatus(nodeName)

	var err error

	for i := 0; i < numContainers; i++ {
		// get slice of card names from the CSV list of container nr i
		cardNames := strings.Split(containerCards[i], ",")
		numCards := len(cardNames)

		if numCards > 0 && len(containerCards[i]) > 0 {
			request := containerRequests[i].newCopy()
			_ = request.divide(numCards)

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
	}

	return nil
}

// This must be called with rwmutex locked
// set add=true to add, false to remove resources.
func (c *Cache) adjustPodResources(pod *v1.Pod, adj bool, annotation, nodeName string) error {
	// get slice of resource maps, one map per container
	containerRequests := containerRequests(pod)

	// get slice of card name lists, one CSV list per container
	containerCards := strings.Split(annotation, "|")

	// we need to be atomic, either all succeed or none succeed, so check first
	err := c.checkPodResourceAdjustment(containerRequests, nodeName, containerCards, adj)
	if err != nil {
		return err
	}

	// now that we have checked, error checks are omitted below
	numContainers := len(containerRequests)
	for i := 0; i < numContainers; i++ {
		// get slice of card names from the CSV list of container nr i
		cardNames := strings.Split(containerCards[i], ",")
		numCards := len(cardNames)

		if numCards > 0 && len(containerCards[i]) > 0 {
			_ = containerRequests[i].divide(numCards)

			if _, ok := c.nodeStatuses[nodeName]; !ok {
				c.nodeStatuses[nodeName] = nodeResources{}
			}

			for _, cardName := range cardNames {
				_, ok := c.nodeStatuses[nodeName][cardName]
				if !ok {
					c.nodeStatuses[nodeName][cardName] = resourceMap{}
				}

				if adj { // add
					_ = c.nodeStatuses[nodeName][cardName].addRM(containerRequests[i])
				} else {
					_ = c.nodeStatuses[nodeName][cardName].subtractRM(containerRequests[i])
				}
			}
		}
	}

	if adj { // add
		c.annotatedPods[getKey(pod)] = annotation
	} else {
		delete(c.annotatedPods, getKey(pod))
	}

	c.printNodeStatus(nodeName)

	return nil
}

func signalHandler() (stopChannel <-chan struct{}) {
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

// calculateLabelChanges returns a map of added or updated labels, and a map of deleted labels.
func calculateLabelChanges(
	node *v1.Node, oldLabels map[string]string) (added, updated, deleted map[string]string) {
	added = map[string]string{}
	updated = map[string]string{}
	deleted = map[string]string{}

	for label, value := range node.Labels {
		if strings.HasPrefix(label, tasNSPrefix) {
			parts := strings.Split(label, "/")
			if len(parts) == 2 &&
				strings.HasPrefix(parts[1], gpuDescheduleLabelPrefix) {
				if oldValue, ok := oldLabels[label]; !ok {
					added[label] = value
				} else if value != oldValue {
					updated[label] = value
				}
			}
		}
	}

	for label, value := range oldLabels {
		if _, ok := node.Labels[label]; !ok {
			deleted[label] = value
		}
	}

	return added, updated, deleted
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

func (c *Cache) updateNodeInCache(oldNodeObj, newNodeObj interface{}) {
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
	switch t := nodeObj.(type) {
	case *v1.Node:
		node = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		node, ok = t.Obj.(*v1.Node)

		if !ok {
			klog.Warningf("cannot convert to *v1.Node: %v", t.Obj)

			return
		}
	default:
		klog.Warningf("cannot convert to *v1.Node: %v", t)

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
	annotation, ok := pod.Annotations[cardAnnotationName]
	if !ok {
		return
	}

	item := podWorkQueueItem{
		ns:         pod.Namespace,
		name:       pod.Name,
		annotation: annotation,
		pod:        pod,
		action:     podAdded,
	}
	c.podWorkQueue.Add(item)
}

func (c *Cache) updatePodInCache(oldPodObj, newPodObj interface{}) {
	newPod, ok := newPodObj.(*v1.Pod)
	if !ok {
		klog.Warningf("conversion of newObj -> pod failed: %v", newPodObj)

		return
	}

	// if POD does not have the necessary annotation, can't work on it yet
	annotation, ok := newPod.Annotations[cardAnnotationName]
	if !ok {
		return
	}

	item := podWorkQueueItem{
		name:       newPod.Name,
		ns:         newPod.Namespace,
		annotation: annotation,
		pod:        newPod,
		action:     podUpdated,
	}

	// Change action to completed if pod is completed
	if isCompletedPod(newPod) {
		item.action = podCompleted
	}

	c.podWorkQueue.Add(item)
}

func (c *Cache) deletePodFromCache(podObj interface{}) {
	klog.V(L4).Infof("deletePodFromCache")
	c.rwmutex.RLock() // reads c.annotatedPods
	klog.V(L5).Infof("deletePodFromCache locked")
	defer c.rwmutex.RUnlock()

	var pod *v1.Pod
	switch t := podObj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)

		if !ok {
			klog.Warningf("cannot convert to *v1.Pod: %v", t.Obj)

			return
		}
	default:
		klog.Warningf("cannot convert to *v1.Pod: %v", t)

		return
	}

	key := getKey(pod)
	_, annotatedPod := c.annotatedPods[key]

	klog.V(L4).Infof("delete pod %s in ns %s annotated:%v", pod.Name, pod.Namespace, annotatedPod)

	if !annotatedPod {
		return
	}

	item := podWorkQueueItem{
		ns:     pod.Namespace,
		name:   pod.Name,
		pod:    pod,
		action: podDeleted,
	}
	c.podWorkQueue.Add(item)
}

func (c *Cache) startNodeWork(stopChannel <-chan struct{}) {
	defer c.nodeWorkQueue.ShutDown()
	defer runtime.HandleCrash()

	klog.V(L2).Info("starting node worker")

	// block calling goroutine
	wait.Until(c.nodeWorkerRun, workerWaitTime, stopChannel)

	klog.V(L2).Info("node worker shutting down")
}

// This steals the calling goroutine and blocks doing work.
func (c *Cache) startPodWork(stopChannel <-chan struct{}) {
	defer c.podWorkQueue.ShutDown()
	defer runtime.HandleCrash()

	klog.V(L2).Info("starting pod worker")

	// block calling goroutine
	wait.Until(c.podWorkerRun, workerWaitTime, stopChannel)

	klog.V(L2).Info("pod worker shutting down")
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
	klog.V(L5).Info("node worker started")

	itemI, quit := c.nodeWorkQueue.Get()

	if quit {
		klog.V(L2).Info("node worker quitting")

		return false
	}

	defer c.nodeWorkQueue.Done(itemI)
	defer klog.V(L5).Info("node worker ended work")

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
	klog.V(L5).Info("pod worker started")

	itemI, quit := c.podWorkQueue.Get()

	if quit {
		klog.V(L2).Info("pod worker quitting")

		return false
	}

	defer c.podWorkQueue.Done(itemI)
	defer klog.V(L5).Info("pod worker ended work")

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

// GetNodeResourceStatus returns a copy of current resource status for a node (map of per card resource maps).
func (c *Cache) getNodeResourceStatus(nodeName string) nodeResources {
	klog.V(L4).Infof("getNodeResourceStatus %v", nodeName)
	c.rwmutex.RLock()
	klog.V(L5).Infof("getNodeResourceStatus %v locked", nodeName)
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

// stripPrefixesFromLabels strips prefixed namespaces and prefixes from labels. Non-prefixed labels are filtered out.
func stripPrefixesFromLabels(nsPrefix, labelPrefix string, nodeLabels map[string]string) map[string]string {
	strippedLabels := map[string]string{}

	for key, value := range nodeLabels {
		if strings.HasPrefix(key, nsPrefix) {
			parts := strings.Split(key, "/")
			if len(parts) == maxLabelParts {
				name := parts[1]

				if strings.HasPrefix(name, labelPrefix) {
					strippedLabels[name[len(labelPrefix):]] = value
				}
			}
		}
	}

	return strippedLabels
}

// handlePodLabeling adds labels to the pod based on whether the pod uses related GPU or not. Also cleans up
// unneeded pod-labels, although that is a rare event as such since the current labels ought to result in pod
// being descheduled. The basic use case here is to catch node labels indicating that a GPU is in a bad state,
// and to actually find the related PODs and to label them for the external descheduler.
func (c *Cache) handlePodLabeling(
	node *v1.Node, pod *v1.Pod, newNodeLabels, updatedNodeLabels, deletedNodeLabels map[string]string) {
	podGPUs := allPodGPUs(pod)

	// get stripped card names from node labels
	newCardsForDescheduling := stripPrefixesFromLabels(tasNSPrefix, gpuDescheduleLabelPrefix, newNodeLabels)
	// add grouped GPUs, if that is requested in the label values
	addPCIGroupGPUs(node, newCardsForDescheduling)
	// loop through cards, see if gpu is in use
	payload := []patchValue{}

	for cardName := range newCardsForDescheduling {
		if podGPUs[cardName] {
			payload = append(payload, patchValue{
				Op:    "add",
				Path:  "/metadata/labels/" + podDescheduleString,
				Value: "gpu",
			})
		}
	}

	// updating if labels would change from one value to another before descheduling happens (corner case)
	updatedCardsForDescheduling := stripPrefixesFromLabels(tasNSPrefix, gpuDescheduleLabelPrefix, updatedNodeLabels)
	if len(updatedCardsForDescheduling) > 0 {
		addPCIGroupGPUs(node, updatedCardsForDescheduling)

		for cardName := range updatedCardsForDescheduling {
			if podGPUs[cardName] {
				payload = append(payload, patchValue{
					Op:    "replace",
					Path:  "/metadata/labels/" + podDescheduleString,
					Value: "gpu",
				})
			}
		}
	}

	// label removal, in case descheduler wasn't fast enough to pick it and deschedule
	cardsOkToUseAgain := stripPrefixesFromLabels(tasNSPrefix, gpuDescheduleLabelPrefix, deletedNodeLabels)
	for cardName := range cardsOkToUseAgain {
		if podGPUs[cardName] {
			payload = append(payload, patchValue{
				Op:    "remove",
				Path:  "/metadata/labels/" + podDescheduleString,
				Value: "",
			})
		}
	}

	if len(payload) > 0 {
		payloadBytes, _ := json.Marshal(payload)

		_, err := c.clientset.CoreV1().Pods(pod.GetNamespace()).Patch(
			context.TODO(), pod.GetName(), types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
		if err == nil {
			klog.V(L4).Infof("Pod %s labeled successfully.", pod.GetName())
		} else {
			klog.Errorf("Pod %s labeling failed.", pod.GetName())
		}
	}
}

func (c *Cache) updateKnownNodeLabels(nodeName string, added, updated, deleted map[string]string) {
	if _, ok := c.knownNodeLabels[nodeName]; !ok {
		c.knownNodeLabels[nodeName] = map[string]string{}
	}

	for key := range deleted {
		delete(c.knownNodeLabels[nodeName], key)
	}

	for key, value := range added {
		c.knownNodeLabels[nodeName][key] = value
	}

	for key, value := range updated {
		c.knownNodeLabels[nodeName][key] = value
	}
}

func (c *Cache) handleNode(item nodeWorkQueueItem) error {
	klog.V(L4).Infof("handleNode %s", item.nodeName)

	switch item.action {
	case nodeAdded:
		fallthrough
	case nodeUpdated:
		added, updated, deleted := calculateLabelChanges(item.node, c.knownNodeLabels[item.nodeName])
		// add and remove related labels
		if len(added)+len(updated)+len(deleted) == 0 {
			return nil
		}

		selector, err := fields.ParseSelector("spec.nodeName=" + item.nodeName +
			",status.phase=" + string(v1.PodRunning))
		// gofumpt: do not delete this line
		if err != nil {
			klog.Error(err.Error())

			return err
		}

		runningPodList, err := c.clientset.CoreV1().Pods(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			FieldSelector: selector.String(),
		})
		// gofumpt: do not delete this line
		if err != nil {
			klog.Error(err.Error())

			return err
		}

		for i := range runningPodList.Items {
			c.handlePodLabeling(item.node, &runningPodList.Items[i], added, updated, deleted)
		}

		// update known label list
		c.updateKnownNodeLabels(item.nodeName, added, updated, deleted)
	case nodeDeleted:
		if _, ok := c.knownNodeLabels[item.nodeName]; ok {
			c.knownNodeLabels[item.nodeName] = map[string]string{}
		}
	}

	return nil
}

func (c *Cache) handlePod(item podWorkQueueItem) (forget bool, err error) {
	klog.V(L4).Infof("handlePod %s in ns %s", item.name, item.ns)

	c.rwmutex.Lock() // adjusts podresources
	klog.V(L5).Infof("handlePod %v locked", item.name)
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
			err = c.adjustPodResources(item.pod, remove, item.annotation, item.pod.Spec.NodeName)
		} else {
			msg += "podDeleted, key:" + key + " annotation already gone"
		}
	case podAdded:
		msg += "podAdded -> "

		fallthrough
	case podUpdated:
		_, alreadyAnnotated := c.annotatedPods[key]
		if alreadyAnnotated {
			msg += "podUpdated, key:" + key + " annotation already present"
		} else {
			msg += "podUpdated, key:" + key + " annotation:" + item.annotation
			err = c.adjustPodResources(item.pod, add, item.annotation, item.pod.Spec.NodeName)
		}
	default:
		msg = "unknown action"
		err = errUnknownAction
	}

	klog.V(L4).Infof(msg)

	c.printNodeStatus(item.pod.Spec.NodeName)

	return true, err
}

func (c *Cache) printNodeStatus(nodeName string) {
	if klog.V(L4).Enabled() {
		klog.Info(nodeName, ":")
		resources, ok := c.nodeStatuses[nodeName]

		if ok {
			for key, value := range resources {
				klog.Info("    ", key, ":", value)
			}
		}
	}
}
