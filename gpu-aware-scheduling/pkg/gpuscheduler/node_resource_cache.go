// inspired by https://github.com/AliyunContainerService/gpushare-scheduler-extender

package gpuscheduler

import (
	"errors"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	v1 "k8s.io/api/core/v1"
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
	add              = true
	remove           = false
	workerWaitTime   = time.Millisecond * 100
	informerInterval = time.Second * 30
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

// Cache : basically all things cached, including the resulting resource usage statuses per card
// Nodes cache is needed for incoming scheduling request so that not all node objects need to be
// sent for every scheduled pod.
// Pods cache is needed during the scheduling request so that not all pods need to be read from
// all nodes for every scheduled pod.
// The cache could be accessed from multiple goroutines and therefore needs concurrency protection,
// which is achieved with a mutex.
type Cache struct {
	clientset             kubernetes.Interface
	sharedInformerFactory informers.SharedInformerFactory
	nodeLister            corev1.NodeLister
	workQueue             workqueue.RateLimitingInterface
	podLister             corev1.PodLister
	annotatedPods         map[string]string
	rwmutex               sync.RWMutex
	nodeStatuses          map[string]nodeResources
}

// Node resources = a map of resourceMaps accessed by node card names.
type nodeResources map[string]resourceMap

const /*action*/ (
	podUpdated = iota
	podAdded
	podDeleted
	podCompleted
)

type workQueueItem struct {
	name       string
	ns         string
	annotation string
	action     int
	pod        *v1.Pod
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
		workQueue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "podWorkQueue"),
		podLister:             podLister,
		annotatedPods:         make(map[string]string),
		nodeStatuses:          make(map[string]nodeResources),
	}

	podInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: c.filter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addPodToCache,
			UpdateFunc: c.updatePodInCache,
			DeleteFunc: c.deletePodFromCache,
		},
	})

	go func() { c.startWorking(stopChannel) }()

	return &c
}

func (c *Cache) filter(obj interface{}) bool {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = obj.(*v1.Pod)
	case cache.DeletedFinalStateUnknown:
		pod = t.Obj.(*v1.Pod)
	default:
		return false
	}

	return hasGPUResources(pod)
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

	item := workQueueItem{
		ns:         pod.Namespace,
		name:       pod.Name,
		annotation: annotation,
		pod:        pod,
		action:     podAdded,
	}
	c.workQueue.Add(item)
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

	item := workQueueItem{
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

	c.workQueue.Add(item)
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

	item := workQueueItem{
		ns:     pod.Namespace,
		name:   pod.Name,
		pod:    pod,
		action: podDeleted,
	}
	c.workQueue.Add(item)
}

// This steals the calling goroutine and blocks doing work.
func (c *Cache) startWorking(stopChannel <-chan struct{}) {
	defer c.workQueue.ShutDown()
	defer runtime.HandleCrash()

	klog.V(L2).Info("Starting worker")

	// block calling goroutine
	wait.Until(c.workerRun, workerWaitTime, stopChannel)

	klog.V(L2).Info("Worker shutting down")
}

func (c *Cache) workerRun() {
	for c.work() {
	}
}

func (c *Cache) work() bool {
	klog.V(L3).Info("worker started")

	itemI, quit := c.workQueue.Get()

	if quit {
		klog.V(L3).Info("worker quitting")

		return false
	}

	defer c.workQueue.Done(itemI)
	defer klog.V(L3).Info("worker ended work")

	item := itemI.(workQueueItem)
	forget, err := c.handlePod(item)

	if err == nil {
		if forget {
			c.workQueue.Forget(itemI)
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
	return c.nodeLister.Get(nodeName)
}

func (c *Cache) fetchPod(ns, name string) (*v1.Pod, error) {
	var podCopy *v1.Pod

	nsLister := c.podLister.Pods(ns)
	pod, err := nsLister.Get(name)

	if err == nil {
		podCopy = pod.DeepCopy()
	}

	return podCopy, err
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

func (c *Cache) handlePod(item workQueueItem) (forget bool, err error) {
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
