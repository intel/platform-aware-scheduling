// Package gpuscheduler has the logic for the scheduler extender - including the server it starts and filter methods
package gpuscheduler

import (
	"context"
	"encoding/json"
	"errors"
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
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

//nolint:gomnd
const (
	tsAnnotationName   = "gas-ts"
	cardAnnotationName = "gas-container-cards"
	updateErrorStr     = "please apply your changes to the latest version"
	updateRetryCount   = 5
	gpuListLabel       = "gpu.intel.com/cards"
	gpuPluginResource  = "gpu.intel.com/i915"
	L1                 = klog.Level(1)
	L2                 = klog.Level(2)
	L3                 = klog.Level(3)
	L4                 = klog.Level(4)
	L5                 = klog.Level(5)
)

//nolint: gochecknoglobals // only mocked APIs are allowed as globals
var (
	iClient ClientAPI
	iCache  CacheAPI
)

// Errors.
var (
	errNotFound  = errors.New("not found")
	errEmptyBody = errors.New("request body empty")
	errDecode    = errors.New("error decoding request")
	errWontFit   = errors.New("will not fit")
)

//nolint: gochecknoinits // only mocked APIs are allowed in here
func init() {
	iClient = &clientAPI{}
	iCache = &cacheAPI{}
}

// GASExtender is the scheduler extension part.
type GASExtender struct {
	cache     *Cache
	clientset kubernetes.Interface
	rwmutex   sync.RWMutex
}

// NewGASExtender returns a new GAS Extender.
func NewGASExtender(clientset kubernetes.Interface) *GASExtender {
	return &GASExtender{
		cache:     iCache.NewCache(clientset),
		clientset: clientset,
	}
}

func addAnnotations(ts, annotation string, pod *v1.Pod) {
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	pod.Annotations[tsAnnotationName] = ts
	pod.Annotations[cardAnnotationName] = annotation
}

func (m *GASExtender) annotatePodBind(annotation string, pod *v1.Pod) error {
	var err error

	podCopy := pod.DeepCopy()

	ts := strconv.FormatInt(time.Now().UnixNano(), 10)
	addAnnotations(ts, annotation, podCopy)

	for i := 0; i < updateRetryCount; i++ {
		_, err = iClient.UpdatePod(m.clientset, podCopy)
		if err != nil && strings.Contains(err.Error(), updateErrorStr) {
			// retry
			var err2 error
			podCopy, err2 = iClient.GetPod(m.clientset, podCopy.Namespace, podCopy.Name)

			if err2 != nil {
				klog.Error("pod refresh failed")

				break // pod refresh failed, so bail
			}

			addAnnotations(ts, annotation, podCopy)
			klog.Error("pod update failed, retrying with refreshed pod")

			continue // the usual update error, try again with refreshed pod
		}

		break
	}

	if err != nil {
		klog.Error("Failed to annotate POD with container cards:" + err.Error())
	} else {
		klog.V(L2).Infof("Annotated pod %v with annotation %v", pod.Name, annotation)
	}

	return err
}

// This returns the value of the resource registered by the gpu plugin to the kubelet.
func getPluginResource(resources resourceMap) int64 {
	for resName, value := range resources {
		if strings.HasPrefix(resName, gpuPluginResource) {
			return value
		}
	}

	return 0
}

func getNodeGPUList(node *v1.Node) []string {
	if node == nil || node.Labels == nil {
		klog.Error("No labels in node")

		return nil
	}

	annotation, ok := node.Labels[gpuListLabel]

	if !ok {
		klog.Error("gpulist label not found from node")

		return nil
	}

	return strings.Split(annotation, ".")
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

	_ = perGPUCapacity.divide(gpuCount)

	return perGPUCapacity
}

func getPerGPUResourceRequest(containerRequest resourceMap) (resourceMap, int64) {
	perGPUResourceRequest := containerRequest.newCopy()

	numI915 := getNumI915(containerRequest)

	if numI915 > 1 {
		_ = perGPUResourceRequest.divide(int(numI915))
	}

	return perGPUResourceRequest, numI915
}

func getNumI915(containerRequest resourceMap) int64 {
	if numI915, ok := containerRequest[gpuPluginResource]; ok && numI915 > 0 {
		return numI915
	}

	return 0
}

func (m *GASExtender) getCardsForContainerGPURequest(containerRequest, perGPUCapacity resourceMap,
	nodeName, podName string,
	nodeResourcesUsed nodeResources,
	gpuMap map[string]bool) (cards []string, err error) {
	cards = []string{}

	if len(containerRequest) == 0 {
		return []string{}, nil
	}

	// figure out container resources per gpu
	perGPUResourceRequest, numI915 := getPerGPUResourceRequest(containerRequest)

	for gpuNum := int64(0); gpuNum < numI915; gpuNum++ {
		fitted := false
		// sort keys to iterate always in same order
		gpuNames := make([]string, len(nodeResourcesUsed))
		i := 0

		for gpuName := range nodeResourcesUsed {
			gpuNames[i] = gpuName
			i++
		}

		sort.Strings(gpuNames)

		for _, gpuName := range gpuNames {
			usedResMap := nodeResourcesUsed[gpuName]
			klog.V(L4).Info("Checking gpu ", gpuName)

			if !gpuMap[gpuName] {
				klog.Warningf("node %v gpu %v has vanished", nodeName, gpuName)

				continue
			}

			if checkResourceCapacity(perGPUResourceRequest, perGPUCapacity, usedResMap) {
				err := usedResMap.addRM(perGPUResourceRequest)

				if err == nil {
					fitted = true

					cards = append(cards, gpuName)
				}

				break
			}
		}

		if !fitted {
			klog.V(L4).Infof("pod %v will not fit node %v", podName, nodeName)

			return nil, errWontFit
		}
	}

	return cards, nil
}

func createGPUMap(gpus []string) map[string]bool {
	gpuMap := map[string]bool{}

	for _, gpu := range gpus {
		gpuMap[gpu] = true
	}

	return gpuMap
}

func addEmptyResourceMaps(gpus []string, nodeResourcesUsed nodeResources) {
	for _, gpu := range gpus {
		if _, ok := nodeResourcesUsed[gpu]; !ok {
			nodeResourcesUsed[gpu] = resourceMap{}
		}
	}
}

// runSchedulingLogic searches for the cards for a given pod from a given node. The cards are returned as an annotation
// string. If the pod can't be scheduled in the given node, an error is returned. Note that calling this does not change
// node resource status yet by any means.
func (m *GASExtender) runSchedulingLogic(pod *v1.Pod, nodeName string) (annotation string, err error) {
	podName := pod.Name
	node, err := iCache.FetchNode(m.cache, nodeName)
	// gofumpt: do not delete this line
	if err != nil {
		klog.Warningf("Node %s couldn't be read or node vanished", nodeName)

		return "", err
	}

	gpus := getNodeGPUList(node)
	klog.V(L4).Info("Node gpu list:", gpus)
	gpuCount := len(gpus)

	if gpuCount == 0 {
		klog.Warningf("Node %s GPUs have vanished", nodeName)

		return "", errWontFit
	}

	perGPUCapacity := getPerGPUResourceCapacity(node, gpuCount)
	nodeResourcesUsed, err := m.readNodeResources(nodeName)
	// gofumpt: do not delete this line
	if err != nil {
		klog.Warningf("Node %s resources couldn't be read or node vanished", nodeName)

		return "", err
	}

	gpuMap := createGPUMap(gpus)
	// add empty resourcemaps for cards which have no resources used yet
	addEmptyResourceMaps(gpus, nodeResourcesUsed)

	// select GPUs. Trivial implementation selects first suitable GPUs
	containerRequests := containerRequests(pod)
	containerDelimeter := ""

	for i, containerRequest := range containerRequests {
		cards, err := m.getCardsForContainerGPURequest(containerRequest,
			perGPUCapacity, nodeName, podName, nodeResourcesUsed, gpuMap)
		if err != nil {
			klog.Errorf("container %v out of %v did not fit", i+1, len(containerRequests))

			return "", err
		}

		annotation += containerDelimeter
		cardDelimeter := ""

		for _, card := range cards {
			annotation += cardDelimeter + card
			cardDelimeter = ","
		}

		containerDelimeter = "|"
	}

	return annotation, nil
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
			klog.V(L4).Info(" no capacity available for ", resName)

			return false
		}

		resUsed := used[resName] // missing = 0, default value is ok

		if resUsed < 0 {
			klog.Error("negative amount of resources in use")

			return false
		}

		klog.V(L4).Info(" resource ", resName, " capacity:", strconv.FormatInt(resCapacity, 10), " used:",
			strconv.FormatInt(resUsed, 10), " need:", strconv.FormatInt(resNeed, 10))

		if resUsed+resNeed < 0 {
			klog.Error("resource request overflow error")

			return false
		}

		if resCapacity < resUsed+resNeed {
			klog.V(L4).Info(" not enough resources")

			return false
		}
	}

	klog.V(L4).Info(" there is enough resources")

	return true
}

func (m *GASExtender) bindNode(args *extender.BindingArgs) *extender.BindingResult {
	result := extender.BindingResult{}

	pod, err := m.cache.fetchPod(args.PodNamespace, args.PodName)
	if err != nil {
		klog.Warningf("Pod %s couldn't be read or pod vanished", args.PodName)

		result.Error = err.Error()

		return &result
	}

	m.rwmutex.Lock()
	klog.V(L5).Infof("bind %v:%v to node %v locked", args.PodNamespace, args.PodName, args.Node)
	defer m.rwmutex.Unlock()

	resourcesAdjusted := false
	annotation := ""

	defer func() { // deferred errorhandler
		if err != nil {
			klog.Error("binding failed:", err.Error())
			result.Error = err.Error()

			if resourcesAdjusted {
				// Restore resources to cache. Removing resources should not fail if adding was ok.
				_ = m.cache.adjustPodResourcesL(pod, remove, annotation, args.Node)
			}
		}
	}()

	// pod should always fit, but one never knows if something bad happens between filtering and binding
	annotation, err = m.runSchedulingLogic(pod, args.Node)

	if err != nil {
		return &result
	}

	klog.V(L3).Infof("bind %v:%v to node %v annotation %v", args.PodNamespace, args.PodName, args.Node, annotation)
	err = m.cache.adjustPodResourcesL(pod, add, annotation, args.Node)

	if err != nil {
		return &result
	}

	resourcesAdjusted = true
	err = m.annotatePodBind(annotation, pod) // annotate POD with per-container GPU selection

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

	failedNodes := extender.FailedNodesMap{}
	result := extender.FilterResult{}

	if args.NodeNames == nil || len(*args.NodeNames) == 0 {
		result.Error = "No nodes to compare. " +
			"This should not happen, perhaps the extender is misconfigured with NodeCacheCapable == false."
		klog.Error(result.Error)

		return &result
	}

	m.rwmutex.Lock()
	klog.V(L5).Infof("filter %v:%v from %v locked", args.Pod.Namespace, args.Pod.Name, *args.NodeNames)
	defer m.rwmutex.Unlock()

	for _, nodeName := range *args.NodeNames {
		if _, err := m.runSchedulingLogic(&args.Pod, nodeName); err == nil {
			nodeNames = append(nodeNames, nodeName)
		} else {
			failedNodes[nodeName] = "Not enough GPU-resources for deployment"
		}
	}

	result = extender.FilterResult{
		NodeNames:   &nodeNames,
		FailedNodes: failedNodes,
		Error:       "",
	}

	return &result
}

// decodeRequest reads the json request into the given interface args.
// It returns an error if the request is not in the required format.
func (m *GASExtender) decodeRequest(args interface{}, r *http.Request) error {
	if r.Body == nil {
		return errEmptyBody
	}

	if klog.V(L5).Enabled() {
		requestDump, err := httputil.DumpRequest(r, true)
		if err == nil {
			klog.Infof("http-request:\n%v", string(requestDump))
		}
	}

	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&args); err != nil {
		return errDecode
	}

	return r.Body.Close()
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
	klog.V(L4).Info("filter request received")

	extenderArgs := extender.Args{}
	err := m.decodeRequest(&extenderArgs, r)
	// gofumpt: do not delete this line
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
}

// Bind binds the pod to the node.
func (m *GASExtender) Bind(w http.ResponseWriter, r *http.Request) {
	klog.V(L4).Info("bind request received")

	extenderArgs := extender.BindingArgs{}
	err := m.decodeRequest(&extenderArgs, r)
	// gofumpt: do not delete this line
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
