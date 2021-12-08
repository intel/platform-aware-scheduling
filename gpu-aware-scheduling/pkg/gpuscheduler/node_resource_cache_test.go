// +build !validation

// nolint:testpackage
package gpuscheduler

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/fake"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	properName       = "proper name"
	properAnnotation = "proper annotation"
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

func TestCacheFilters(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	c := NewCache(clientset)

	Convey("When the object is wrong type", t, func() {
		s := "wrong object"
		result := c.podFilter(s)
		So(result, ShouldBeFalse)
		result = c.nodeFilter(s)
		So(result, ShouldBeFalse)
	})
	Convey("When the object is DeleteFinalStateUnknown", t, func() {
		unknown := cache.DeletedFinalStateUnknown{
			Key: "unknown",
			Obj: &v1.Pod{},
		}
		result := c.podFilter(unknown)
		So(result, ShouldBeFalse)
		unknown.Obj = &v1.Node{}
		result = c.nodeFilter(unknown)
		So(result, ShouldBeFalse)
	})
}

func TestPodCacheEventFunctions(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	c := NewCache(clientset)
	badType := "bad type"

	Convey("When trying to add a non-pod object to cache", t, func() {
		wqLen := c.podWorkQueue.Len()
		c.addPodToCache(badType)
		So(c.podWorkQueue.Len(), ShouldEqual, wqLen)
	})

	// annotated pod doesn't always get to cache during validation run,
	// so let's do that here always
	Convey("When a pod with a proper annotation is added to the cache", t, func() {
		wqLen := c.podWorkQueue.Len()
		pod := v1.Pod{}
		pod.Annotations = map[string]string{}
		pod.Annotations[cardAnnotationName] = properAnnotation
		c.addPodToCache(&pod)
		So(c.podWorkQueue.Len(), ShouldEqual, wqLen+1)
	})
	Convey("When trying to update a non-pod object in cache", t, func() {
		wqLen := c.podWorkQueue.Len()
		c.updatePodInCache(badType, badType)
		So(c.podWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When trying to delete a non-pod object from cache", t, func() {
		wqLen := c.podWorkQueue.Len()
		c.deletePodFromCache(badType)
		So(c.podWorkQueue.Len(), ShouldEqual, wqLen)
	})

	unknown := cache.DeletedFinalStateUnknown{
		Key: "unknown",
		Obj: "bad type",
	}

	Convey("When trying to delete a non-pod state-unknown-object from cache", t, func() {
		wqLen := c.podWorkQueue.Len()
		c.deletePodFromCache(unknown)
		So(c.podWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When deleting a proper POD from a proper namespace with a proper annotation", t, func() {
		wqLen := c.podWorkQueue.Len()
		pod := v1.Pod{}
		pod.Name = properName
		pod.Namespace = properName
		c.annotatedPods[getKey(&pod)] = properAnnotation
		c.deletePodFromCache(&pod)
		So(c.podWorkQueue.Len(), ShouldEqual, wqLen+1)
	})
}

func TestNodeCacheEventFunctions(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	c := NewCache(clientset)
	badType := "bad type"

	Convey("When trying to add a non-node object to cache", t, func() {
		wqLen := c.nodeWorkQueue.Len()
		c.addNodeToCache(badType)
		So(c.nodeWorkQueue.Len(), ShouldEqual, wqLen)
	})

	Convey("When trying to update a non-node object in cache", t, func() {
		wqLen := c.nodeWorkQueue.Len()
		c.updateNodeInCache(badType, badType)
		So(c.nodeWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When trying to delete a non-node object from cache", t, func() {
		wqLen := c.nodeWorkQueue.Len()
		c.deleteNodeFromCache(badType)
		So(c.nodeWorkQueue.Len(), ShouldEqual, wqLen)
	})

	unknown := cache.DeletedFinalStateUnknown{
		Key: "unknown",
		Obj: "bad type",
	}

	Convey("When trying to delete a non-node state-unknown-object from cache", t, func() {
		wqLen := c.nodeWorkQueue.Len()
		c.deleteNodeFromCache(unknown)
		So(c.nodeWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When deleting a proper Node from cache", t, func() {
		wqLen := c.nodeWorkQueue.Len()
		node := v1.Node{}
		node.Name = properName
		c.deleteNodeFromCache(&node)
		So(c.nodeWorkQueue.Len(), ShouldEqual, wqLen+1)
	})
}

func TestHandlePodError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	c := NewCache(clientset)
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
		forget, err := c.handlePod(item)
		So(forget, ShouldBeTrue)
		So(err, ShouldNotBeNil)
	})

	Convey("When I call HandlePod with podAdded action", t, func() {
		item.action = podAdded
		forget, err := c.handlePod(item)
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
	}
}

func TestPodWork(t *testing.T) {
	// to be able to call work() directly, we need a mock cache which doesn't call work() itself
	c := createMockCache()

	Convey("When working on a bad pod", t, func() {
		wqLen := c.podWorkQueue.Len()
		badPod := v1.Pod{}
		item := podWorkQueueItem{
			action: -1,
			pod:    &badPod,
		}
		c.podWorkQueue.Add(item)
		So(c.podWorkQueue.Len(), ShouldEqual, wqLen+1)
		ret := c.podWork()
		So(ret, ShouldBeTrue)
		So(c.podWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When the work queue is shutting down", t, func() {
		c.podWorkQueue.ShutDown()
		ret := c.podWork()
		So(ret, ShouldBeFalse)
	})
}

func TestNodeWork(t *testing.T) {
	// to be able to call work() directly, we need a mock cache which doesn't call work() itself
	c := createMockCache()

	Convey("When working on a bad node", t, func() {
		wqLen := c.nodeWorkQueue.Len()
		badNode := v1.Node{}
		item := nodeWorkQueueItem{
			action: -1,
			node:   &badNode,
		}
		c.nodeWorkQueue.Add(item)
		So(c.nodeWorkQueue.Len(), ShouldEqual, wqLen+1)
		ret := c.nodeWork()
		So(ret, ShouldBeTrue)
		So(c.nodeWorkQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When the node work queue is shutting down", t, func() {
		c.nodeWorkQueue.ShutDown()
		ret := c.nodeWork()
		So(ret, ShouldBeFalse)
	})
}
