// +build !validation

// nolint:testpackage
package gpuscheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/intel/platform-aware-scheduling/extender"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	nodename = "nodename"
)

func getDummyExtender(objects ...runtime.Object) *GASExtender {
	clientset := fake.NewSimpleClientset(objects...)

	return NewGASExtender(clientset, true, true)
}

func getFakePod() *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"gas-ts": "1"},
		},
		Spec: *getMockPodSpec(),
	}
}

func getMockPodSpec() *v1.PodSpec {
	return &v1.PodSpec{
		Containers: []v1.Container{
			{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"gpu.intel.com/i915": resource.MustParse("1"),
					},
				},
			},
		},
	}
}

func getMockNode(cardNames ...string) *v1.Node {
	if len(cardNames) == 0 {
		cardNames = []string{"card0"}
	}

	node := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{},
			Name:   "mocknode",
		},
		Status: v1.NodeStatus{
			Capacity:    v1.ResourceList{},
			Allocatable: v1.ResourceList{},
		},
	}

	cardCount := strconv.Itoa(len(cardNames))
	node.Status.Capacity["gpu.intel.com/i915"] = resource.MustParse(cardCount)
	node.Status.Allocatable["gpu.intel.com/i915"] = resource.MustParse(cardCount)

	delim := ""

	cardNameList := ""
	for _, cardName := range cardNames {
		cardNameList += delim + cardName
		delim = ","
	}

	node.Labels["gpu.intel.com/cards"] = cardNameList

	return &node
}

func TestNewGASExtender(t *testing.T) {
	Convey("When I create a new gas extender", t, func() {
		Convey("and InClusterConfig returns an error", func() {
			gas := NewGASExtender(nil, false, false)
			So(gas.clientset, ShouldBeNil)
		})
	})
}

func TestGetPluginResource(t *testing.T) {
	rm := resourceMap{}

	Convey("When I call getPluginResource with an empty map", t, func() {
		i915Count := getPluginResource(rm)
		So(i915Count, ShouldEqual, 0)
	})
}

func TestSchedulingLogicBadParams(t *testing.T) {
	gas := getDummyExtender()
	pod := v1.Pod{}
	mockCache := MockCacheAPI{}
	origCacheAPI := iCache
	iCache = &mockCache

	Convey("When I call getAnnotationForPodGPURequest with empty params", t, func() {
		mockCache.On("FetchNode", mock.Anything, mock.Anything).Return(nil, errMock).Once()
		result, _, err := gas.runSchedulingLogic(&pod, "")
		So(result, ShouldEqual, "")
		So(err, ShouldNotBeNil)
	})

	iCache = origCacheAPI
}

type testWriter struct {
	headerStatus int
}

var errMock = errors.New("mock error")

func (t *testWriter) Header() http.Header {
	return http.Header{}
}

func (t *testWriter) Write([]byte) (int, error) {
	return 0, errMock
}

func (t *testWriter) WriteHeader(statusCode int) {
	t.headerStatus = statusCode
}

func TestErrorHandler(t *testing.T) {
	w := testWriter{headerStatus: 0}

	Convey("When error handler is called", t, func() {
		gas := getDummyExtender()

		gas.errorHandler(&w, nil)
		So(w.headerStatus, ShouldEqual, http.StatusNotFound)
	})
}

func TestResourceCheck(t *testing.T) {
	capacity := resourceMap{}
	used := resourceMap{}
	need := resourceMap{"foo": 1}

	Convey("When need exceeds capacity", t, func() {
		result := checkResourceCapacity(need, capacity, used)
		So(result, ShouldEqual, false)
	})
}

func TestReadNodeResources(t *testing.T) {
	mockCache := MockCacheAPI{}
	iCache = &mockCache

	Convey("When cache is nil", t, func() {
		mockCache.On("NewCache", mock.Anything).Return(nil)
		mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{})
		gas := getDummyExtender()
		resources, err := gas.readNodeResources("mocknode")
		So(err, ShouldBeNil)
		So(len(resources), ShouldEqual, 0)
	})
}

func TestFilterNodes(t *testing.T) {
	gas := getDummyExtender()
	args := extender.Args{}

	Convey("When there are no nodes to compare when filtering", t, func() {
		result := gas.filterNodes(&args)
		So(result.Error, ShouldNotEqual, "")
	})

	args.NodeNames = &[]string{nodename}
	mockCache := MockCacheAPI{}
	origCacheAPI := iCache
	iCache = &mockCache

	Convey("When node can't be read", t, func() {
		mockCache.On("FetchNode", mock.Anything, (*args.NodeNames)[0]).Return(nil, errMock).Once()
		result := gas.filterNodes(&args)
		So(len(*result.NodeNames), ShouldEqual, 0)
	})

	Convey("When node resources can't be read", t, func() {
		mockCache.On("FetchNode", mock.Anything, (*args.NodeNames)[0]).Return(nil, nil).Once()
		mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nil)
		result := gas.filterNodes(&args)
		So(len(*result.NodeNames), ShouldEqual, 0)
	})

	iCache = origCacheAPI
}

func TestBindNode(t *testing.T) {
	pod := getFakePod()

	gas := getDummyExtender(pod)
	mockCache := MockCacheAPI{}
	origCacheAPI := iCache
	iCache = &mockCache
	args := extender.BindingArgs{}

	Convey("When the args are empty", t, func() {
		mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(nil, errMock).Once()
		result := gas.bindNode(&args)
		So(result.Error, ShouldNotEqual, "")
	})

	args.Node = nodename

	Convey("When node can't be read", t, func() {
		mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{}, nil).Once()
		mockCache.On("FetchNode", mock.Anything, args.Node).Return(nil, errMock).Once()
		result := gas.bindNode(&args)
		So(result.Error, ShouldNotBeNil)
	})

	Convey("When node can be read, but has no capacity", t, func() {
		mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
			Spec: *getMockPodSpec(),
		}, nil).Once()
		mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"gpu.intel.com/cards": "card0",
				},
			},
		}, nil).Once()
		mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
		result := gas.bindNode(&args)
		So(result.Error, ShouldEqual, "will not fit")
	})

	Convey("When node can be read, and it has capacity", t, func() {
		mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
			Spec: *getMockPodSpec(),
		}, nil).Once()
		mockCache.On("FetchNode", mock.Anything, args.Node).Return(getMockNode(), nil).Once()
		mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
		mockCache.On("AdjustPodResourcesL",
			mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
		result := gas.bindNode(&args)
		So(result.Error, ShouldEqual, "")
	})

	iCache = origCacheAPI
}

func TestAllowlist(t *testing.T) {
	pod := getFakePod()

	gas := getDummyExtender(pod)
	mockCache := MockCacheAPI{}
	origCacheAPI := iCache
	iCache = &mockCache
	args := extender.BindingArgs{}
	args.Node = nodename

	for _, cardName := range []string{"card0", "card1"} {
		cardName := cardName

		Convey("When pod has an allowlist and the node card is in it", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"gas-allow": cardName},
				},
				Spec: *getMockPodSpec(),
			}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(getMockNode(), nil).Once()
			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
			result := gas.bindNode(&args)
			if cardName == "card0" {
				So(result.Error, ShouldEqual, "")
			} else {
				So(result.Error, ShouldEqual, "will not fit")
			}
		})
	}

	iCache = origCacheAPI
}

func TestDenylist(t *testing.T) {
	pod := getFakePod()

	gas := getDummyExtender(pod)
	mockCache := MockCacheAPI{}
	origCacheAPI := iCache
	iCache = &mockCache
	args := extender.BindingArgs{}
	args.Node = nodename

	for _, cardName := range []string{"card0", "card1"} {
		cardName := cardName

		Convey("When pod has a denylist", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"gas-deny": cardName},
				},
				Spec: *getMockPodSpec(),
			}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"gpu.intel.com/cards": "card0"}},
				Status: v1.NodeStatus{
					Capacity:    v1.ResourceList{"gpu.intel.com/i915": resource.MustParse("1")},
					Allocatable: v1.ResourceList{"gpu.intel.com/i915": resource.MustParse("1")},
				},
			}, nil).Once()
			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
			result := gas.bindNode(&args)
			if cardName != "card0" {
				So(result.Error, ShouldEqual, "")
			} else {
				So(result.Error, ShouldEqual, "will not fit")
			}
		})
	}

	iCache = origCacheAPI
}

func TestGPUDisabling(t *testing.T) {
	pod := getFakePod()

	gas := getDummyExtender(pod)
	mockCache := MockCacheAPI{}
	origCacheAPI := iCache
	iCache = &mockCache
	args := extender.BindingArgs{}
	args.Node = nodename

	for _, labelValue := range []string{pciGroupValue, "true"} {
		labelValue := labelValue

		Convey("When node has a disable-label and the node card is in it", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				Spec: *getMockPodSpec(),
			}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"gpu.intel.com/cards": "card0",
						tasNSPrefix + "policy/" + gpuDisableLabelPrefix + "card0": labelValue,
						pciGroupLabel: "0",
					},
				},
				Status: v1.NodeStatus{
					Capacity:    v1.ResourceList{"gpu.intel.com/i915": resource.MustParse("1")},
					Allocatable: v1.ResourceList{"gpu.intel.com/i915": resource.MustParse("1")},
				},
			}, nil).Once()
			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
			result := gas.bindNode(&args)
			So(result.Error, ShouldEqual, "will not fit")
		})
	}

	iCache = origCacheAPI
}

func TestWriteResponse(t *testing.T) {
	gas := getDummyExtender()

	Convey("When writeResponse is called with nil response", t, func() {
		w := testWriter{}
		gas.writeResponse(&w, nil)
		So(w.headerStatus, ShouldEqual, http.StatusBadRequest)
	})
}

func TestDecodeRequest(t *testing.T) {
	gas := getDummyExtender()

	Convey("When decoding something not really JSON", t, func() {
		request, err := http.NewRequestWithContext(context.Background(),
			"POST", "http://foo/bar", bytes.NewBuffer([]byte("foo")))
		So(err, ShouldBeNil)
		request.Header.Set("Content-Type", "application/json")
		err = gas.decodeRequest("foo", request)
		So(err, ShouldNotBeNil)
	})
}

func TestPreferredGPU(t *testing.T) {
	gas := getDummyExtender()
	node := getMockNode("card0", "card1", "card2")

	pod := getFakePod()

	containerRequest := resourceMap{"gpu.intel.com/i915": 1}
	perGPUCapacity := resourceMap{"gpu.intel.com/i915": 1}

	nodeResourcesUsed := nodeResources{"card0": resourceMap{}, "card1": resourceMap{}, "card2": resourceMap{}}
	gpuMap := map[string]bool{"card0": true, "card1": true, "card2": true}

	Convey("When a gpu is not preferred, alphabetically first gpu should be selected", t, func() {
		cards, preferred, err := gas.getCardsForContainerGPURequest(containerRequest, perGPUCapacity,
			node, pod,
			nodeResourcesUsed,
			gpuMap)

		So(len(cards), ShouldEqual, 1)
		So(cards[0], ShouldEqual, "card0")
		So(err, ShouldBeNil)
		So(preferred, ShouldBeFalse)
	})

	Convey("When a gpu is preferred, it should be selected", t, func() {
		node.Labels["telemetry.aware.scheduling.policy/gas-prefer-gpu"] = "card2"
		cards, preferred, err := gas.getCardsForContainerGPURequest(containerRequest, perGPUCapacity,
			node, pod,
			nodeResourcesUsed,
			gpuMap)

		So(len(cards), ShouldEqual, 1)
		So(cards[0], ShouldEqual, "card2")
		So(err, ShouldBeNil)
		So(preferred, ShouldBeTrue)
	})
}

func TestFilter(t *testing.T) {
	gas := getDummyExtender()

	Convey("When Filter is called", t, func() {
		w := testWriter{}
		r := http.Request{}
		Convey("when args are fine but request body is empty", func() {
			r.Method = http.MethodPost
			r.ContentLength = 100
			r.Header = http.Header{}
			r.Header.Set("Content-Type", "application/json")
			gas.Filter(&w, &r)
		})
		Convey("when args are fine but request body is ok", func() {
			content, err := json.Marshal(map[string]string{"foo": "bar"})
			So(err, ShouldBeNil)
			request, err := http.NewRequestWithContext(context.Background(),
				"POST", "http://foo/bar", bytes.NewBuffer(content))
			So(err, ShouldBeNil)
			request.Header.Set("Content-Type", "application/json")
			gas.Filter(&w, request)
		})
	})
}

func TestBind(t *testing.T) {
	gas := getDummyExtender()

	mockCache := MockCacheAPI{}
	origCacheAPI := iCache
	iCache = &mockCache

	Convey("When Bind is called", t, func() {
		w := testWriter{}
		r := http.Request{}
		Convey("when args are fine but request body is empty", func() {
			r.Method = http.MethodPost
			r.ContentLength = 100
			r.Header = http.Header{}
			r.Header.Set("Content-Type", "application/json")
			gas.Bind(&w, &r)
		})
		Convey("when args are fine but request body is ok", func() {
			content, err := json.Marshal(map[string]string{"foo": "bar"})
			So(err, ShouldBeNil)
			request, err := http.NewRequestWithContext(context.Background(),
				"POST", "http://foo/bar", bytes.NewBuffer(content))
			So(err, ShouldBeNil)
			request.Header.Set("Content-Type", "application/json")
			mockCache.On("FetchPod", mock.Anything, mock.Anything, mock.Anything).Return(nil, errMock).Once()
			gas.Bind(&w, request)
		})
	})

	iCache = origCacheAPI
}

func TestGetNodeGPUList(t *testing.T) {
	node := v1.Node{}

	Convey("When I try to get the node gpu list with a node that doesn't have labels", t, func() {
		list := getNodeGPUList(&node)
		So(list, ShouldBeNil)
	})
	Convey("When I try to get the node gpu list with a node that doesn't have the correct label", t, func() {
		node.Labels = map[string]string{}
		list := getNodeGPUList(&node)
		So(list, ShouldBeNil)
	})
}
