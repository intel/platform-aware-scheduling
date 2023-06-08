// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

//go:build !validation
// +build !validation

//nolint:testpackage
package gpuscheduler

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	properName       = "proper name"
	properAnnotation = "proper annotation"
	trueValueString  = "true"
)

func TestNewCache(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	Convey("When I create a new cache", t, func() {
		cach := NewCache(clientset)
		So(cach, ShouldNotBeNil)
		mockInternalCacheAPI := MockInternalCacheAPI{}
		defer func() { internCacheAPI = &internalCacheAPI{} }()
		internCacheAPI = &mockInternalCacheAPI
		Convey("But when waitforcachesync fails", func() {
			mockInternalCacheAPI.On("WaitForCacheSync", mock.Anything, mock.Anything).Return(false).Once()
			cach = NewCache(clientset)
			So(cach, ShouldBeNil)
		})
		Convey("But when waitforcachesync fails on the second call", func() {
			mockInternalCacheAPI.On("WaitForCacheSync", mock.Anything, mock.Anything).Return(true).Once()
			mockInternalCacheAPI.On("WaitForCacheSync", mock.Anything, mock.Anything).Return(false).Once()
			cach = NewCache(clientset)
			So(cach, ShouldBeNil)
		})
	})
}

//nolint:gochecknoglobals // only test resource
var dummyCache *Cache

func (c *Cache) reset() {
	c.annotatedPods = map[string]string{}
	c.nodeStatuses = map[string]nodeResources{}
	c.nodeTileStatuses = map[string]nodeTiles{}
	c.previousDeschedCards = map[string][]string{}
	c.previousDeschedTiles = map[string][]string{}
	c.podDeschedStatuses = map[string]bool{}
}

func getDummyCache() *Cache {
	if dummyCache == nil {
		dummyCache = NewCache(fake.NewSimpleClientset())
	}

	dummyCache.reset()

	return dummyCache
}

func TestCacheFilters(t *testing.T) {
	dummyCache := getDummyCache()

	Convey("When the object is wrong type", t, func() {
		s := "wrong object"
		result := dummyCache.podFilter(s)
		So(result, ShouldBeFalse)
		result = dummyCache.nodeFilter(s)
		So(result, ShouldBeFalse)
	})
	Convey("When the object is DeleteFinalStateUnknown", t, func() {
		unknown := cache.DeletedFinalStateUnknown{
			Key: "unknown",
			Obj: &v1.Pod{},
		}
		result := dummyCache.podFilter(unknown)
		So(result, ShouldBeFalse)
		unknown.Obj = &v1.Node{}
		result = dummyCache.nodeFilter(unknown)
		So(result, ShouldBeFalse)
	})
}

func TestPodCacheEventFunctions(t *testing.T) {
	// we need a mock cache which doesn't call work() itself to avoid race conditions at work queue length checks
	mockCache := createMockCache()
	badType := "bad type"

	Convey("When trying to add a non-pod object to cache", t, func() {
		wqLen := mockCache.podWorkQueue.Len()
		mockCache.addPodToCache(badType)
		So(mockCache.podWorkQueue.Len(), ShouldEqual, wqLen)
	})

	// annotated pod doesn't always get to cache during validation run,
	// so let's do that here always
	Convey("When a pod with a proper annotation is added to the cache", t, func() {
		wqLen := mockCache.podWorkQueue.Len()
		pod := v1.Pod{}
		pod.Annotations = map[string]string{}
		pod.Annotations[cardAnnotationName] = properAnnotation
		mockCache.addPodToCache(&pod)
		So(mockCache.podWorkQueue.Len(), ShouldEqual, wqLen+1)
	})
	Convey("When trying to update a non-pod object in cache", t, func() {
		wqLen := mockCache.podWorkQueue.Len()
		mockCache.updatePodInCache(badType, badType)
		So(mockCache.podWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When trying to delete a non-pod object from cache", t, func() {
		wqLen := mockCache.podWorkQueue.Len()
		mockCache.deletePodFromCache(badType)
		So(mockCache.podWorkQueue.Len(), ShouldEqual, wqLen)
	})

	unknown := cache.DeletedFinalStateUnknown{
		Key: "unknown",
		Obj: "bad type",
	}

	Convey("When trying to delete a non-pod state-unknown-object from cache", t, func() {
		wqLen := mockCache.podWorkQueue.Len()
		mockCache.deletePodFromCache(unknown)
		So(mockCache.podWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When deleting a proper POD from a proper namespace with a proper annotation", t, func() {
		wqLen := mockCache.podWorkQueue.Len()
		pod := v1.Pod{}
		pod.Name = properName
		pod.Namespace = properName
		mockCache.annotatedPods[getKey(&pod)] = properAnnotation
		mockCache.deletePodFromCache(&pod)
		So(mockCache.podWorkQueue.Len(), ShouldEqual, wqLen+1)
	})
}

func TestNodeCacheEventFunctions(t *testing.T) {
	// we need a mock cache which doesn't call work() itself to avoid race conditions at work queue length checks
	mockCache := createMockCache()
	badType := "bad type"

	Convey("When trying to add a non-node object to cache", t, func() {
		wqLen := mockCache.nodeWorkQueue.Len()
		mockCache.addNodeToCache(badType)
		So(mockCache.nodeWorkQueue.Len(), ShouldEqual, wqLen)
	})

	Convey("When trying to update a non-node object in cache", t, func() {
		wqLen := mockCache.nodeWorkQueue.Len()
		mockCache.updateNodeInCache(badType, badType)
		So(mockCache.nodeWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When trying to delete a non-node object from cache", t, func() {
		wqLen := mockCache.nodeWorkQueue.Len()
		mockCache.deleteNodeFromCache(badType)
		So(mockCache.nodeWorkQueue.Len(), ShouldEqual, wqLen)
	})

	unknown := cache.DeletedFinalStateUnknown{
		Key: "unknown",
		Obj: "bad type",
	}

	Convey("When trying to delete a non-node state-unknown-object from cache", t, func() {
		wqLen := mockCache.nodeWorkQueue.Len()
		mockCache.deleteNodeFromCache(unknown)
		So(mockCache.nodeWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When deleting a proper Node from cache", t, func() {
		wqLen := mockCache.nodeWorkQueue.Len()
		node := v1.Node{}
		node.Name = properName
		mockCache.deleteNodeFromCache(&node)
		So(mockCache.nodeWorkQueue.Len(), ShouldEqual, wqLen+1)
	})
}

func TestHandlePodError(t *testing.T) {
	dummyCache := getDummyCache()
	item := podWorkQueueItem{
		action: -1,
		pod: &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								"gpu.intel.com/i915": resource.MustParse("1"),
							},
						},
					},
				},
				NodeName: "TestNode",
			},
		},
	}

	Convey("When I call HandlePod with a bad action", t, func() {
		forget, err := dummyCache.handlePod(item)
		So(forget, ShouldBeTrue)
		So(err, ShouldNotBeNil)
	})

	Convey("When I call HandlePod with podAdded action", t, func() {
		item.action = podAdded
		forget, err := dummyCache.handlePod(item)
		So(forget, ShouldBeTrue)
		So(err, ShouldBeNil)
	})
}

func createMockCache() *Cache {
	return &Cache{
		clientset:             nil,
		sharedInformerFactory: nil,
		nodeLister:            nil,
		podWorkQueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "podWorkQueue"),
		nodeWorkQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "nodeWorkQueue"),
		podLister:             nil,
		annotatedPods:         make(map[string]string),
		nodeStatuses:          make(map[string]nodeResources),
		nodeTileStatuses:      make(map[string]nodeTiles),
	}
}

func TestPodWork(t *testing.T) {
	// to be able to call work() directly, we need a mock cache which doesn't call work() itself
	cache := createMockCache()

	Convey("When working on a bad pod", t, func() {
		wqLen := cache.podWorkQueue.Len()
		badPod := v1.Pod{}
		item := podWorkQueueItem{
			action: -1,
			pod:    &badPod,
		}
		cache.podWorkQueue.Add(item)
		So(cache.podWorkQueue.Len(), ShouldEqual, wqLen+1)
		ret := cache.podWork()
		So(ret, ShouldBeTrue)
		So(cache.podWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When the work queue is shutting down", t, func() {
		cache.podWorkQueue.ShutDown()
		ret := cache.podWork()
		So(ret, ShouldBeFalse)
	})
}

func TestNodeWork(t *testing.T) {
	// to be able to call work() directly, we need a mock cache which doesn't call work() itself
	cache := createMockCache()

	Convey("When working on a bad node", t, func() {
		wqLen := cache.nodeWorkQueue.Len()
		badNode := v1.Node{}
		item := nodeWorkQueueItem{
			action: -1,
			node:   &badNode,
		}
		cache.nodeWorkQueue.Add(item)
		So(cache.nodeWorkQueue.Len(), ShouldEqual, wqLen+1)
		ret := cache.nodeWork()
		So(ret, ShouldBeTrue)
		So(cache.nodeWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When the node work queue is shutting down", t, func() {
		cache.nodeWorkQueue.ShutDown()
		ret := cache.nodeWork()
		So(ret, ShouldBeFalse)
	})
}

func TestAdjustTiles(t *testing.T) {
	dummyCache := getDummyCache()

	Convey("When node's tile statuses doesn't exist yet", t, func() {
		dummyCache.nodeTileStatuses = make(map[string]nodeTiles)
		dummyCache.adjustTiles(true, "node1", "card0:gt0+gt1")

		statuses, ok := dummyCache.nodeTileStatuses["node1"]
		So(ok, ShouldEqual, true)

		tileInfo, ok := statuses["card0"]
		So(ok, ShouldEqual, true)

		So(0, ShouldBeIn, tileInfo)
		So(1, ShouldBeIn, tileInfo)
	})

	Convey("When node's tile statuses are updated", t, func() {
		dummyCache.nodeTileStatuses = make(map[string]nodeTiles)
		dummyCache.adjustTiles(true, "node1", "card0:gt0+gt1")
		dummyCache.adjustTiles(true, "node1", "card0:gt0+gt1+gt3")

		statuses := dummyCache.nodeTileStatuses["node1"]
		tileInfo := statuses["card0"]
		So(0, ShouldBeIn, tileInfo)
		So(1, ShouldBeIn, tileInfo)
		So(3, ShouldBeIn, tileInfo)
		So(4, ShouldNotBeIn, tileInfo)
	})

	Convey("When node's tile statuses are removed", t, func() {
		dummyCache.nodeTileStatuses = make(map[string]nodeTiles)
		dummyCache.adjustTiles(true, "node1", "card0:gt0+gt1")
		dummyCache.adjustTiles(false, "node1", "card0:gt0")

		statuses := dummyCache.nodeTileStatuses["node1"]
		tileInfo := statuses["card0"]
		So(0, ShouldNotBeIn, tileInfo)
		So(1, ShouldBeIn, tileInfo)
	})

	Convey("When second gpu's tiles are reserved", t, func() {
		dummyCache.nodeTileStatuses = make(map[string]nodeTiles)
		dummyCache.adjustTiles(true, "node1", "card0:gt0+gt1")
		dummyCache.adjustTiles(true, "node1", "card1:gt3+gt4")

		statuses := dummyCache.nodeTileStatuses["node1"]

		_, ok := statuses["card0"]
		So(ok, ShouldEqual, true)

		_, ok = statuses["card1"]
		So(ok, ShouldEqual, true)

		tiles := statuses["card1"]
		So(3, ShouldBeIn, tiles)
		So(4, ShouldBeIn, tiles)
	})

	Convey("When everything is reserved and released", t, func() {
		dummyCache.nodeTileStatuses = make(map[string]nodeTiles)
		dummyCache.adjustTiles(true, "node1", "card0:gt0+gt1")
		dummyCache.adjustTiles(true, "node1", "card1:gt3+gt4")

		dummyCache.adjustTiles(false, "node1", "card1:gt3+gt4")
		dummyCache.adjustTiles(false, "node1", "card0:gt0+gt1")

		statuses := dummyCache.nodeTileStatuses["node1"]

		tiles, ok := statuses["card0"]
		So(ok, ShouldEqual, true)
		So(len(tiles), ShouldEqual, 0)

		tiles, ok = statuses["card1"]
		So(ok, ShouldEqual, true)
		So(len(tiles), ShouldEqual, 0)
	})
}

func TestAdjustPodResources(t *testing.T) {
	dummyCache := getDummyCache()

	pod := v1.Pod{}
	podContainer := v1.Container{Name: "foobarContainer"}
	podRequests := v1.ResourceRequirements{
		Requests: v1.ResourceList{
			"gpu.intel.com/tiles": resource.MustParse("1"),
			"gpu.intel.com/i915":  resource.MustParse("1"),
		},
	}
	podContainer.Resources = podRequests
	pod.Spec.Containers = append(pod.Spec.Containers, podContainer)

	Convey("When adjusting pod resources with pod with one container", t, func() {
		dummyCache.nodeTileStatuses = make(map[string]nodeTiles)
		err := dummyCache.adjustPodResources(&pod, true, "card0", "card0:gt0", "node1")

		So(err, ShouldEqual, nil)

		statuses, ok := dummyCache.nodeTileStatuses["node1"]
		So(ok, ShouldEqual, true)

		tiles, ok := statuses["card0"]
		So(ok, ShouldEqual, true)
		So(len(tiles), ShouldEqual, 1)
		So(0, ShouldBeIn, tiles)
	})

	Convey("When adjusting pod resources back and forth", t, func() {
		dummyCache.nodeTileStatuses = make(map[string]nodeTiles)
		err1 := dummyCache.adjustPodResources(&pod, true, "card0", "card0:gt0", "node1")
		err2 := dummyCache.adjustPodResources(&pod, false, "card0", "card0:gt0", "node1")

		statuses := dummyCache.nodeTileStatuses["node1"]

		tiles, ok := statuses["card0"]
		So(ok, ShouldEqual, true)
		So(len(tiles), ShouldEqual, 0)
		So(err1, ShouldBeNil)
		So(err2, ShouldBeNil)
	})

	Convey("When adjusting pod resources via L", t, func() {
		dummyCache.nodeTileStatuses = make(map[string]nodeTiles)
		err := dummyCache.adjustPodResourcesL(&pod, true, "card0", "card0:gt0", "node1")

		So(err, ShouldEqual, nil)

		statuses, ok := dummyCache.nodeTileStatuses["node1"]
		So(ok, ShouldEqual, true)

		tiles, ok := statuses["card0"]
		So(ok, ShouldEqual, true)
		So(len(tiles), ShouldEqual, 1)
		So(0, ShouldBeIn, tiles)
	})
}

func TestGetTileIndices(t *testing.T) {
	Convey("When ok tiles are converted into indices", t, func() {
		tileStrings := []string{
			"gt0", "gt1",
		}

		ret := getTileIndices(tileStrings)

		So(len(ret), ShouldEqual, 2)
		So(0, ShouldBeIn, ret)
		So(1, ShouldBeIn, ret)
	})

	Convey("When bad tiles are converted into indices", t, func() {
		tileStrings := []string{
			"gt", "gtX",
		}

		ret := getTileIndices(tileStrings)

		So(len(ret), ShouldEqual, 0)
	})
}

func GetNewDummyNode(name string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				// group cards 0 and 1 together
				"gpu.intel.com/pci-groups": "0.1",
			},
			Name: name,
		},
		Status: v1.NodeStatus{
			Capacity:    v1.ResourceList{},
			Allocatable: v1.ResourceList{},
		},
	}
}

func TestDeschedulingCards(t *testing.T) {
	clientset := fake.NewSimpleClientset(&v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind: "pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod1",
			Annotations: map[string]string{
				"gas-container-cards": "card0",
				"gas-container-tiles": "card0:gt0",
			},
			Labels: map[string]string{},
		},
	})

	applied := 0
	applyCheck := func(action k8stesting.Action) (bool, runtime.Object, error) {
		patchAction, ok := action.(k8stesting.PatchAction)
		if !ok {
			return false, nil, nil
		}

		requiredStr := "\"value\":\"gpu\",\"op\":\"add\",\"path\""
		patch := patchAction.GetPatch()
		patchStr := string(patch)

		if !strings.Contains(patchStr, requiredStr) {
			return true, nil, errNotFound
		}

		applied++

		return true, nil, nil
	}

	removed := 0
	removeCheck := func(action k8stesting.Action) (bool, runtime.Object, error) {
		patchAction, ok := action.(k8stesting.PatchAction)
		if !ok {
			return false, nil, nil
		}

		requiredStr := "\"value\":\"\",\"op\":\"remove\",\"path\""
		patch := patchAction.GetPatch()
		patchStr := string(patch)

		if !strings.Contains(patchStr, requiredStr) {
			return true, nil, errNotFound
		}

		removed++

		return true, nil, nil
	}

	cach := NewCache(clientset)

	mockInternalCacheAPI := MockInternalCacheAPI{}

	defer func() {
		internCacheAPI = &internalCacheAPI{}
	}()

	internCacheAPI = &mockInternalCacheAPI

	var item nodeWorkQueueItem
	item.action = nodeUpdated
	item.nodeName = "foobarNode"
	item.node = GetNewDummyNode(item.nodeName)

	testLabels := map[string]string{
		"telemetry.aware.scheduling.foo/gas-deschedule-pods-card0":     trueValueString,
		"telemetry.aware.scheduling.foo/gas-deschedule-pods-card1":     "PCI_GROUP",
		"telemetry.aware.scheduling.foo/gas-tile-deschedule-card0_gt0": trueValueString,
	}

	for labelName, labelValue := range testLabels {
		Convey("When node updates with descheduling labels", t, func() {
			// simulate TAS labeling node with descheduling need
			item.node.Labels[labelName] = labelValue

			applied = 0
			clientset.Fake.PrependReactor("patch", "pods", applyCheck)
			err := cach.handleNode(item)
			clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

			So(err, ShouldBeNil)
			So(applied, ShouldEqual, 1)

			// simulate TAS removing the previous descheduling need
			delete(item.node.Labels, labelName)

			removed = 0
			clientset.Fake.PrependReactor("patch", "pods", removeCheck)
			err = cach.handleNode(item)
			clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

			So(err, ShouldBeNil)
			So(removed, ShouldEqual, 1)
		})
	}

	Convey("When node doesn't get descheduled due to other tile's deschedule", t, func() {
		// simulate TAS labeling node with descheduling need
		item.node.Labels["telemetry.aware.scheduling.foo/gas-tile-disable-card0_gt1"] = trueValueString

		applied = 0
		clientset.Fake.PrependReactor("patch", "pods", applyCheck)
		err := cach.handleNode(item)
		clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

		So(err, ShouldBeNil)
		So(applied, ShouldEqual, 0)

		// simulate TAS removing the previous descheduling need
		delete(item.node.Labels, "telemetry.aware.scheduling.foo/gas-tile-disable-card0_gt1")

		removed = 0
		clientset.Fake.PrependReactor("patch", "pods", removeCheck)
		err = cach.handleNode(item)
		clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

		So(err, ShouldBeNil)
		So(removed, ShouldEqual, 0)
	})

	Convey("When node remains descheduled until all labels are removed", t, func() {
		// simulate TAS labeling node with descheduling needs
		item.node.Labels["telemetry.aware.scheduling.foo/gas-deschedule-pods-card0"] = trueValueString
		item.node.Labels["telemetry.aware.scheduling.foo/gas-tile-deschedule-card0_gt0"] = trueValueString

		applied = 0
		clientset.Fake.PrependReactor("patch", "pods", applyCheck)
		err := cach.handleNode(item)
		clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

		So(err, ShouldBeNil)
		So(applied, ShouldEqual, 1)

		// simulate TAS removing one of the descheduling needs
		delete(item.node.Labels, "telemetry.aware.scheduling.foo/gas-deschedule-pods-card0")

		removed = 0
		clientset.Fake.PrependReactor("patch", "pods", removeCheck)
		err = cach.handleNode(item)
		clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

		So(err, ShouldBeNil)
		So(removed, ShouldEqual, 0)

		// simulate TAS removing the last descheduling need
		delete(item.node.Labels, "telemetry.aware.scheduling.foo/gas-tile-deschedule-card0_gt0")

		removed = 0
		clientset.Fake.PrependReactor("patch", "pods", removeCheck)
		err = cach.handleNode(item)
		clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

		So(err, ShouldBeNil)
		So(removed, ShouldEqual, 1)
	})
}
