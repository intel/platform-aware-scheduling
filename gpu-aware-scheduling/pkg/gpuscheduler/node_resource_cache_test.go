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

func TestCacheFilter(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	c := NewCache(clientset)

	Convey("When the object is wrong type", t, func() {
		s := "wrong object"
		result := c.filter(s)
		So(result, ShouldBeFalse)
	})
	Convey("When the object is DeleteFinalStateUnknown", t, func() {
		unknown := cache.DeletedFinalStateUnknown{
			Key: "unknown",
			Obj: &v1.Pod{},
		}
		result := c.filter(unknown)
		So(result, ShouldBeFalse)
	})
}

func TestCacheEventFunctions(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	c := NewCache(clientset)
	badType := "bad type"

	Convey("When trying to add a non-pod object to cache", t, func() {
		wqLen := c.workQueue.Len()
		c.addPodToCache(badType)
		So(c.workQueue.Len(), ShouldEqual, wqLen)
	})

	// annotated pod doesn't always get to cache during validation run,
	// so let's do that here always
	Convey("When a pod with a proper annotation is added to the cache", t, func() {
		wqLen := c.workQueue.Len()
		pod := v1.Pod{}
		pod.Annotations = map[string]string{}
		pod.Annotations[cardAnnotationName] = properAnnotation
		c.addPodToCache(&pod)
		So(c.workQueue.Len(), ShouldEqual, wqLen+1)
	})
	Convey("When trying to update a non-pod object in cache", t, func() {
		wqLen := c.workQueue.Len()
		c.updatePodInCache(badType, badType)
		So(c.workQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When trying to delete a non-pod object from cache", t, func() {
		wqLen := c.workQueue.Len()
		c.deletePodFromCache(badType)
		So(c.workQueue.Len(), ShouldEqual, wqLen)
	})

	unknown := cache.DeletedFinalStateUnknown{
		Key: "unknown",
		Obj: "bad type",
	}

	Convey("When trying to delete a non-pod state-unknown-object from cache", t, func() {
		wqLen := c.workQueue.Len()
		c.deletePodFromCache(unknown)
		So(c.workQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When deleting a proper POD from a proper namespace with a proper annotation", t, func() {
		wqLen := c.workQueue.Len()
		pod := v1.Pod{}
		pod.Name = properName
		pod.Namespace = properName
		c.annotatedPods[getKey(&pod)] = properAnnotation
		c.deletePodFromCache(&pod)
		So(c.workQueue.Len(), ShouldEqual, wqLen+1)
	})
}

func TestHandlePodError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	c := NewCache(clientset)
	item := workQueueItem{
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

func TestWork(t *testing.T) {
	// to be able to call work() directly, we need a mock cache which doesn't call work() itself
	c := Cache{
		clientset:             nil,
		sharedInformerFactory: nil,
		nodeLister:            nil,
		workQueue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "podWorkQueue"),
		podLister:             nil,
		annotatedPods:         make(map[string]string),
		nodeStatuses:          make(map[string]nodeResources),
	}

	Convey("When working on a bad pod", t, func() {
		wqLen := c.workQueue.Len()
		badPod := v1.Pod{}
		item := workQueueItem{
			action: -1,
			pod:    &badPod,
		}
		c.workQueue.Add(item)
		So(c.workQueue.Len(), ShouldEqual, wqLen+1)
		ret := c.work()
		So(ret, ShouldBeTrue)
		So(c.workQueue.Len(), ShouldEqual, wqLen)
	})
	Convey("When the work queue is shutting down", t, func() {
		c.workQueue.ShutDown()
		ret := c.work()
		So(ret, ShouldBeFalse)
	})
}
