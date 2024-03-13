// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

//go:build !validation
// +build !validation

//nolint:testpackage
package gpuscheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
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
	extenderv1 "k8s.io/kube-scheduler/extender/v1"
)

const (
	nodename = "nodename"
	policy   = "policy/"
	card0    = "card0"
	card0Gt0 = "card0_gt0"
)

func getDummyExtender(objects ...runtime.Object) *GASExtender {
	clientset := fake.NewSimpleClientset(objects...)

	return NewGASExtender(clientset, true, true, "")
}

//nolint:gochecknoglobals // only test resource
var emptyExtender *GASExtender

func getEmptyExtender() *GASExtender {
	if emptyExtender == nil {
		emptyExtender = getDummyExtender()
	}

	return emptyExtender
}

func getFakePod(pluginResourceName string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"gas-ts": "1"},
		},
		Spec: *getMockPodSpec(pluginResourceName),
	}
}

func getMockPodSpec(pluginResourceName string) *v1.PodSpec {
	return &v1.PodSpec{
		Containers: []v1.Container{
			{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
					},
				},
			},
		},
	}
}

func getMockPodSpecWithTile(tileCount int, pluginResourceName string) *v1.PodSpec {
	return &v1.PodSpec{
		Containers: []v1.Container{
			{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/tiles":               resource.MustParse(strconv.Itoa(tileCount)),
					},
				},
			},
		},
	}
}

func getMockPodSpecMultiCont(pluginResourceName string) *v1.PodSpec {
	return &v1.PodSpec{
		Containers: []v1.Container{
			{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/tiles":               resource.MustParse("3"),
					},
				},
			},
			{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/tiles":               resource.MustParse("1"),
					},
				},
			},
		},
	}
}

func getMockPodSpecMultiContXeLinked(containerCount int, pluginResourceName string) *v1.PodSpec {
	containers := []v1.Container{}
	for i := 0; i < containerCount; i++ {
		containers = append(containers, v1.Container{
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceName(pluginResourceName): resource.MustParse("2"),
					"gpu.intel.com/tiles":               resource.MustParse("2"),
				},
			},
		})
	}

	return &v1.PodSpec{
		Containers: containers,
	}
}

func getMockPodSpecNCont(containerCount int, pluginResourceName string) *v1.PodSpec {
	containers := []v1.Container{}

	for i := 1; i <= containerCount; i++ {
		container := v1.Container{
			Name: fmt.Sprintf("container%d", i),
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceName(pluginResourceName): resource.MustParse("1"),
					"gpu.intel.com/millicores":          resource.MustParse("100"),
				},
			},
		}
		containers = append(containers, container)
	}

	return &v1.PodSpec{
		Containers: containers,
	}
}

func getMockPodSpecMultiContSamegpu(pluginResourceName string) *v1.PodSpec {
	return &v1.PodSpec{
		Containers: []v1.Container{
			{
				Name: "container1",
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/tiles":               resource.MustParse("2"),
					},
				},
			},
			{
				Name: "container2",
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/memory.max":          resource.MustParse("8589934592"), // 8Gi
					},
				},
			},
			{
				Name: "container3",
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/millicores":          resource.MustParse("200"),
					},
				},
			},
			{
				Name: "container4",
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/millicores":          resource.MustParse("200"),
					},
				},
			},
		},
	}
}

func getMockNode(sharedDevCount, tileCountPerCard int, pluginResourceName string, cardNames ...string) *v1.Node {
	if len(cardNames) == 0 {
		cardNames = []string{card0}
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

	cardCount := strconv.Itoa(len(cardNames) * sharedDevCount)
	tileCount := strconv.Itoa(len(cardNames) * tileCountPerCard)
	node.Status.Capacity[v1.ResourceName(pluginResourceName)] = resource.MustParse(cardCount)
	node.Status.Capacity["gpu.intel.com/tiles"] = resource.MustParse(tileCount)
	node.Status.Allocatable[v1.ResourceName(pluginResourceName)] = resource.MustParse(cardCount)
	node.Status.Allocatable["gpu.intel.com/tiles"] = resource.MustParse(tileCount)

	delim := ""
	cardNameList := ""
	gpuNumList := ""

	for _, cardName := range cardNames {
		cardNameList += delim + cardName
		gpuNumList += delim + cardName[4:]
		delim = "."
	}

	node.Labels[gpuListLabel] = cardNameList
	node.Labels[gpuNumbersLabel] = gpuNumList

	return &node
}

func TestNewGASExtender(t *testing.T) {
	Convey("When I create a new gas extender", t, func() {
		Convey("and InClusterConfig returns an error", func() {
			gas := NewGASExtender(nil, false, false, "")
			So(gas.clientset, ShouldBeNil)
		})
	})
}

func TestSchedulingLogicBadParams(t *testing.T) {
	gas := getEmptyExtender()
	mockCache := MockCacheAPI{}
	origCacheAPI := iCache
	iCache = &mockCache

	Convey("When I call getNodeForName with empty params", t, func() {
		mockCache.On("FetchNode", mock.Anything, mock.Anything).Return(nil, errMock).Once()
		var nilNode *v1.Node
		result, err := gas.getNodeForName("")
		So(result, ShouldEqual, nilNode)
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
	writer := testWriter{headerStatus: 0}

	Convey("When error handler is called", t, func() {
		gas := getEmptyExtender()

		gas.errorHandler(&writer, nil)
		So(writer.headerStatus, ShouldEqual, http.StatusNotFound)
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
	origCacheAPI := iCache
	iCache = &mockCache

	Convey("When cache is nil", t, func() {
		mockCache.On("NewCache", mock.Anything).Return(nil)
		mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{})
		gas := getEmptyExtender()
		resources, err := gas.readNodeResources("mocknode")
		So(err, ShouldBeNil)
		So(len(resources), ShouldEqual, 0)
	})

	iCache = origCacheAPI
}

func TestFilterNodes(t *testing.T) {
	gas := getEmptyExtender()
	args := extenderv1.ExtenderArgs{}

	Convey("When there are no nodes to compare when filtering", t, func() {
		result := gas.filterNodes(&args)
		So(result.Error, ShouldNotEqual, "")
	})

	args.NodeNames = &[]string{nodename}
	args.Pod = &v1.Pod{}
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
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := getFakePod(pluginResourceName)

		gas := getDummyExtender(pod)

		mockCache := MockCacheAPI{}
		origCacheAPI := iCache
		iCache = &mockCache
		args := extenderv1.ExtenderBindingArgs{}
		ctx := context.TODO()

		Convey("When the args are empty", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(nil, errMock).Once()
			result := gas.bindNode(ctx, &args)
			So(result.Error, ShouldNotEqual, "")
		})

		args.Node = nodename

		Convey("When node can't be read", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(nil, errMock).Once()
			result := gas.bindNode(ctx, &args)
			So(result.Error, ShouldNotBeNil)
		})

		Convey("When node can be read, but has no capacity", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				Spec: *getMockPodSpec(pluginResourceName),
			}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"gpu.intel.com/cards": card0,
					},
				},
			}, nil).Once()
			mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(nodeTiles{}, nil).Once()
			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
			result := gas.bindNode(ctx, &args)
			So(result.Error, ShouldEqual, "will not fit")
		})

		Convey("When node can be read, and it has capacity", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				Spec: *getMockPodSpec(pluginResourceName),
			}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(getMockNode(1, 1, pluginResourceName), nil).Once()
			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(nodeTiles{}, nil).Once()
			result := gas.bindNode(ctx, &args)
			So(result.Error, ShouldEqual, "")
		})

		Convey("When pod has invalid UID", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				Spec: *getMockPodSpec(pluginResourceName),
				ObjectMeta: metav1.ObjectMeta{
					UID: "foobar",
				},
			}, nil).Once()
			result := gas.bindNode(ctx, &args)
			So(result.Error, ShouldNotEqual, "")
		})

		iCache = origCacheAPI
	}
}

func TestAllowlist(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := getFakePod(pluginResourceName)

		gas := getDummyExtender(pod)
		mockCache := MockCacheAPI{}
		origCacheAPI := iCache
		iCache = &mockCache
		args := extenderv1.ExtenderBindingArgs{}
		args.Node = nodename
		ctx := context.TODO()

		for _, cardName := range []string{card0, "card1"} {
			cardName := cardName

			Convey("When pod has an allowlist and the node card is in it", t, func() {
				mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{"gas-allow": cardName},
					},
					Spec: *getMockPodSpec(pluginResourceName),
				}, nil).Once()
				mockCache.On("FetchNode", mock.Anything, args.Node).Return(getMockNode(1, 1, pluginResourceName), nil).Once()
				mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
				mockCache.On("AdjustPodResourcesL",
					mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
				mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(nodeTiles{}).Once()
				result := gas.bindNode(ctx, &args)
				if cardName == card0 {
					So(result.Error, ShouldEqual, "")
				} else {
					So(result.Error, ShouldEqual, "will not fit")
				}
			})
		}

		iCache = origCacheAPI
	}
}

func TestDenylist(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := getFakePod(pluginResourceName)

		gas := getDummyExtender(pod)
		mockCache := MockCacheAPI{}
		origCacheAPI := iCache
		iCache = &mockCache
		args := extenderv1.ExtenderBindingArgs{}
		args.Node = nodename
		ctx := context.TODO()

		for _, cardName := range []string{card0, "card1"} {
			cardName := cardName

			Convey("When pod has a denylist", t, func() {
				mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{"gas-deny": cardName},
					},
					Spec: *getMockPodSpec(pluginResourceName),
				}, nil).Once()
				mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"gpu.intel.com/cards": card0}},
					Status: v1.NodeStatus{
						Capacity:    v1.ResourceList{v1.ResourceName(pluginResourceName): resource.MustParse("1")},
						Allocatable: v1.ResourceList{v1.ResourceName(pluginResourceName): resource.MustParse("1")},
					},
				}, nil).Once()
				mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
				mockCache.On("AdjustPodResourcesL",
					mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
				mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(nodeTiles{}).Once()
				result := gas.bindNode(ctx, &args)
				if cardName != card0 {
					So(result.Error, ShouldEqual, "")
				} else {
					So(result.Error, ShouldEqual, "will not fit")
				}
			})
		}

		iCache = origCacheAPI
	}
}

func TestGPUDisabling(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := getFakePod(pluginResourceName)

		gas := getDummyExtender(pod)
		mockCache := MockCacheAPI{}
		origCacheAPI := iCache
		iCache = &mockCache
		args := extenderv1.ExtenderBindingArgs{}
		args.Node = nodename
		ctx := context.TODO()

		for _, labelValue := range []string{pciGroupValue, trueValueString} {
			labelValue := labelValue

			Convey("When node has a disable-label and the node card is in it", t, func() {
				mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
					Spec: *getMockPodSpec(pluginResourceName),
				}, nil).Once()
				mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"gpu.intel.com/cards":                                card0,
							tasNSPrefix + policy + gpuDisableLabelPrefix + card0: labelValue,
							pciGroupLabel: "0",
						},
					},
					Status: v1.NodeStatus{
						Capacity:    v1.ResourceList{v1.ResourceName(pluginResourceName): resource.MustParse("1")},
						Allocatable: v1.ResourceList{v1.ResourceName(pluginResourceName): resource.MustParse("1")},
					},
				}, nil).Once()
				mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
				mockCache.On("AdjustPodResourcesL",
					mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
				mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(nodeTiles{}).Once()
				result := gas.bindNode(ctx, &args)
				So(result.Error, ShouldEqual, "will not fit")
			})
		}

		iCache = origCacheAPI
	}
}

func TestWriteResponse(t *testing.T) {
	gas := getEmptyExtender()

	Convey("When writeResponse is called with nil response", t, func() {
		w := testWriter{}
		gas.writeResponse(&w, nil)
		So(w.headerStatus, ShouldEqual, http.StatusBadRequest)
	})
}

func TestDecodeRequest(t *testing.T) {
	gas := getEmptyExtender()

	Convey("When decoding something not really JSON", t, func() {
		request, err := http.NewRequestWithContext(context.Background(),
			http.MethodPost, "http://foo/bar", bytes.NewBufferString("foo"))
		So(err, ShouldBeNil)
		request.Header.Set("Content-Type", "application/json")
		err = gas.decodeRequest("foo", request)
		So(err, ShouldNotBeNil)
	})
}

func TestPreferredGPU(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		gas := getEmptyExtender()
		node := getMockNode(1, 1, pluginResourceName, card0, "card1", "card2")

		pod := getFakePod(pluginResourceName)

		containerRequest := resourceMap{pluginResourceName: 1}
		perGPUCapacity := resourceMap{pluginResourceName: 1}

		nodeResourcesUsed := nodeResources{card0: resourceMap{}, "card1": resourceMap{}, "card2": resourceMap{}}
		gpuMap := map[string]bool{card0: true, "card1": true, "card2": true}

		Convey("When a gpu is not preferred, alphabetically first gpu should be selected", t, func() {
			cards, preferred, err := gas.getCardsForContainerGPURequest(containerRequest, perGPUCapacity,
				node, pod,
				nodeResourcesUsed,
				gpuMap)

			So(len(cards), ShouldEqual, 1)
			So(cards[0], ShouldResemble, Card{
				gpuName:         card0,
				xeLinkedTileIds: []int{},
			})
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
			So(cards[0], ShouldResemble, Card{
				gpuName:         "card2",
				xeLinkedTileIds: []int{},
			})
			So(err, ShouldBeNil)
			So(preferred, ShouldBeTrue)
		})
	}
}

func TestFilter(t *testing.T) {
	gas := getEmptyExtender()

	Convey("When Filter is called", t, func() {
		writer := testWriter{}
		request := http.Request{}
		Convey("when args are fine but request body is empty", func() {
			request.Method = http.MethodPost
			request.ContentLength = 100
			request.Header = http.Header{}
			request.Header.Set("Content-Type", "application/json")
			gas.Filter(&writer, &request)
		})
		Convey("when args are fine but request body is ok", func() {
			content, err := json.Marshal(map[string]string{"foo": "bar"})
			So(err, ShouldBeNil)
			request, err := http.NewRequestWithContext(context.Background(),
				http.MethodPost, "http://foo/bar", bytes.NewBuffer(content))
			So(err, ShouldBeNil)
			request.Header.Set("Content-Type", "application/json")
			gas.Filter(&writer, request)
		})
	})
}

func TestBind(t *testing.T) {
	gas := getEmptyExtender()

	mockCache := MockCacheAPI{}
	origCacheAPI := iCache
	iCache = &mockCache

	Convey("When Bind is called", t, func() {
		writer := testWriter{}
		request := http.Request{}
		Convey("when args are fine but request body is empty", func() {
			request.Method = http.MethodPost
			request.ContentLength = 100
			request.Header = http.Header{}
			request.Header.Set("Content-Type", "application/json")
			gas.Bind(&writer, &request)
		})
		Convey("when args are fine but request body is ok", func() {
			content, err := json.Marshal(map[string]string{"foo": "bar"})
			So(err, ShouldBeNil)
			request, err := http.NewRequestWithContext(context.Background(),
				http.MethodPost, "http://foo/bar", bytes.NewBuffer(content))
			So(err, ShouldBeNil)
			request.Header.Set("Content-Type", "application/json")
			mockCache.On("FetchPod", mock.Anything, mock.Anything, mock.Anything).Return(nil, errMock).Once()
			gas.Bind(&writer, request)
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

func TestGetNodeGPUListFromGpuNumbers(t *testing.T) {
	Convey("When I try to get the node gpu list with new gpu numbers label", t, func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					gpuNumbersLabel: "0.1.2",
				},
			},
		}

		list := getNodeGPUList(&node)
		So(list, ShouldNotBeNil)
		So(list, ShouldResemble, []string{card0, "card1", "card2"})
	})
	Convey("When I try to get the node gpu list from three new gpu numbers labels", t, func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					gpuNumbersLabel:       "0.1.2",
					gpuNumbersLabel + "2": "Z.5.8.9",
					gpuNumbersLabel + "3": "Z.10",
				},
			},
		}

		list := getNodeGPUList(&node)
		So(list, ShouldNotBeNil)
		So(list, ShouldResemble, []string{card0, "card1", "card2", "card5", "card8", "card9", "card10"})
	})
}

func TestCreateTileAnnotation(t *testing.T) {
	gas := getEmptyExtender()
	node := getMockNode(1, 1, card0, "card1", "card2")
	perGPUCapacity := resourceMap{"gpu.intel.com/tiles": 4}

	mockCache := MockCacheAPI{}
	origCacheAPI := iCache
	iCache = &mockCache

	noPreferredTiles := []int{}

	Convey("When I create a simple tile annotation", t, func() {
		noTilesInUse := nodeTiles{card0: []int{}, "card1": []int{}, "card2": []int{}}
		containerRequest := resourceMap{"gpu.intel.com/tiles": 1}

		mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(noTilesInUse).Once()
		result := gas.createTileAnnotation(Card{gpuName: card0}, 1,
			containerRequest, perGPUCapacity, node,
			map[string][]int{}, noPreferredTiles)
		So(len(result), ShouldEqual, len("card0:gt0"))
		assignedIndices := []int{-1, -1, -1, -1}
		expectedIndices := map[int]bool{0: true, 1: true, 2: true, 3: true}
		fmt.Sscanf(result, "card0:gt%d", &assignedIndices[0])
		delete(expectedIndices, assignedIndices[0])
		So(len(expectedIndices), ShouldEqual, 3)

		assignedIndices = []int{-1, -1, -1, -1}
		expectedIndices = map[int]bool{0: true, 1: true, 2: true, 3: true}
		mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(noTilesInUse).Once()
		containerRequest = resourceMap{"gpu.intel.com/tiles": 4}
		result = gas.createTileAnnotation(
			Card{gpuName: "card1"}, 1, containerRequest, perGPUCapacity, node,
			map[string][]int{}, noPreferredTiles)
		fmt.Sscanf(result, "card1:gt%d+gt%d+gt%d+gt%d",
			&assignedIndices[0], &assignedIndices[1], &assignedIndices[2], &assignedIndices[3])
		delete(expectedIndices, assignedIndices[0])
		delete(expectedIndices, assignedIndices[1])
		delete(expectedIndices, assignedIndices[2])
		delete(expectedIndices, assignedIndices[3])
		So(len(expectedIndices), ShouldEqual, 0)
	})

	Convey("When I create two simple tile annotations back to back", t, func() {
		noTilesInUse := nodeTiles{card0: []int{}, "card1": []int{}, "card2": []int{}}
		containerRequest := resourceMap{"gpu.intel.com/tiles": 2}

		mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(noTilesInUse).Once()
		result := gas.createTileAnnotation(
			Card{gpuName: card0}, 1, containerRequest, perGPUCapacity, node,
			map[string][]int{}, noPreferredTiles)
		So(len(result), ShouldEqual, len("card0:gtx+gty"))
		assignedIndices := []int{-1, -1, -1, -1}
		expectedIndices := map[int]bool{0: true, 1: true, 2: true, 3: true}
		fmt.Sscanf(result, "card0:gt%d+gt%d",
			&assignedIndices[0], &assignedIndices[1])
		delete(expectedIndices, assignedIndices[0])
		delete(expectedIndices, assignedIndices[1])
		So(len(expectedIndices), ShouldEqual, 2)

		someTilesInUse := nodeTiles{card0: []int{0, 1}, "card1": []int{}, "card2": []int{}}

		mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(someTilesInUse).Once()
		containerRequest = resourceMap{"gpu.intel.com/tiles": 2}
		result = gas.createTileAnnotation(
			Card{gpuName: card0}, 1, containerRequest, perGPUCapacity, node,
			map[string][]int{}, noPreferredTiles)

		assignedIndices = []int{-1, -1, -1, -1}
		expectedIndices = map[int]bool{2: true, 3: true} // indices 0 and 1 are in use
		fmt.Sscanf(result, "card0:gt%d+gt%d",
			&assignedIndices[0], &assignedIndices[1])
		delete(expectedIndices, assignedIndices[0])
		delete(expectedIndices, assignedIndices[1])
		So(len(expectedIndices), ShouldEqual, 0)
	})

	Convey("When I create tile annotation with one reserved tile in the middle", t, func() {
		middleTileInUse := nodeTiles{card0: []int{1}, "card1": []int{}, "card2": []int{}}
		containerRequest := resourceMap{"gpu.intel.com/tiles": 3}

		mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(middleTileInUse).Once()
		result := gas.createTileAnnotation(
			Card{gpuName: card0}, 1, containerRequest, perGPUCapacity,
			node, map[string][]int{}, noPreferredTiles)
		So(len(result), ShouldEqual, len("card0:gtx+gty+gtz"))
		assignedIndices := []int{-1, -1, -1, -1}
		expectedIndices := map[int]bool{0: true, 2: true, 3: true} // index 1 is in use
		fmt.Sscanf(result, "card0:gt%d+gt%d+gt%d",
			&assignedIndices[0], &assignedIndices[1], &assignedIndices[2])
		delete(expectedIndices, assignedIndices[0])
		delete(expectedIndices, assignedIndices[1])
		delete(expectedIndices, assignedIndices[2])
		So(len(expectedIndices), ShouldEqual, 0)
	})

	iCache = origCacheAPI
}

func TestArrangeGPUNamesPerResourceAvailibility(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		nodeUsedRes := nodeResources{}

		nodeUsedRes[card0] = resourceMap{pluginResourceName: 1, "gpu.intel.com/tiles": 2}
		nodeUsedRes["card1"] = resourceMap{pluginResourceName: 0, "gpu.intel.com/tiles": 0}
		nodeUsedRes["card2"] = resourceMap{pluginResourceName: 1, "gpu.intel.com/tiles": 1}

		Convey("When arranging gpus by tiles, the one with least used tiles is at front", t, func() {
			gpuNames := []string{card0, "card1", "card2"}

			arrangeGPUNamesPerResourceAvailability(nodeUsedRes, gpuNames, "tiles")
			So(gpuNames[0], ShouldEqual, "card1")
			So(gpuNames[1], ShouldEqual, "card2")
			So(gpuNames[2], ShouldEqual, card0)
		})

		Convey("When arranging gpus by unknown, the order of the gpus shouldn't change", t, func() {
			gpuNames := []string{card0, "card1", "card2"}

			arrangeGPUNamesPerResourceAvailability(nodeUsedRes, gpuNames, "unknown")
			So(gpuNames[0], ShouldEqual, card0)
			So(gpuNames[1], ShouldEqual, "card1")
			So(gpuNames[2], ShouldEqual, "card2")
		})
	}
}

func TestResourceBalancedCardsForContainerGPURequest(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		gas := getEmptyExtender()
		gas.balancedResource = "foo"
		node := getMockNode(1, 1, card0, "card1", "card2")

		pod := getFakePod(pluginResourceName)

		containerRequest := resourceMap{pluginResourceName: 1, "gpu.intel.com/foo": 1}
		perGPUCapacity := resourceMap{pluginResourceName: 1, "gpu.intel.com/foo": 4}

		nodeResourcesUsed := nodeResources{
			card0:   resourceMap{"gpu.intel.com/foo": 1},
			"card1": resourceMap{"gpu.intel.com/foo": 2}, "card2": resourceMap{},
		}
		gpuMap := map[string]bool{card0: true, "card1": true, "card2": true}

		Convey("When GPUs are resource balanced, the least consumed GPU should be used", t, func() {
			cards, preferred, err := gas.getCardsForContainerGPURequest(containerRequest, perGPUCapacity,
				node, pod,
				nodeResourcesUsed,
				gpuMap)

			So(len(cards), ShouldEqual, 1)
			So(cards[0], ShouldResemble, Card{
				gpuName:         "card2",
				xeLinkedTileIds: []int{},
			})
			So(err, ShouldBeNil)
			So(preferred, ShouldBeFalse)
		})
	}
}

func TestFilterWithXeLinkedDisabledTiles(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := getFakePod(pluginResourceName)
		pod.Spec = *getMockPodSpecMultiContXeLinked(1, pluginResourceName)
		pod.Annotations[xelinkAnnotationName] = trueValueString

		clientset := fake.NewSimpleClientset(pod)
		gas := NewGASExtender(clientset, false, false, "")

		mockCache := MockCacheAPI{}
		origCacheAPI := iCache
		iCache = &mockCache
		args := extenderv1.ExtenderBindingArgs{}
		args.Node = nodename

		type testCase struct {
			extraLabels    map[string]string
			description    string
			expectedResult bool
		}

		testCases := []testCase{
			{
				description:    "when one tile is disabled and there is one good xe-link left",
				extraLabels:    map[string]string{tasNSPrefix + policy + tileDisableLabelPrefix + card0Gt0: trueValueString},
				expectedResult: false, // node does not fail (is not filtered)
			},
			{
				description: "when two tiles are disabled and there are no good xe-links left",
				extraLabels: map[string]string{
					tasNSPrefix + policy + tileDisableLabelPrefix + card0Gt0:    trueValueString,
					tasNSPrefix + policy + tileDisableLabelPrefix + "card2_gt1": trueValueString,
				},
				expectedResult: true, // node fails (is filtered)
			},
		}

		Convey("When node has four cards with two xelinks and one disabled xe-linked tile, pod should still fit", t, func() {
			for _, testCase := range testCases {
				t.Logf("test %v", testCase.description)

				mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
					Spec: *getMockPodSpecWithTile(1, pluginResourceName),
				}, nil).Once()
				node := v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"gpu.intel.com/gpu-numbers": "0.1.2.3",
							"gpu.intel.com/tiles":       "4",
							xeLinksLabel:                "0.0-1.0_2.1",
							xeLinksLabel + "2":          "Z-3.2",
						},
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceName(pluginResourceName): resource.MustParse("4"),
							"gpu.intel.com/tiles":               resource.MustParse("16"),
						},
						Allocatable: v1.ResourceList{
							v1.ResourceName(pluginResourceName): resource.MustParse("4"),
							"gpu.intel.com/tiles":               resource.MustParse("16"),
						},
					},
				}
				for key, value := range testCase.extraLabels {
					node.Labels[key] = value
				}
				mockCache.On("FetchNode", mock.Anything, args.Node).Return(&node, nil).Once()

				usedResources := nodeResources{card0: resourceMap{pluginResourceName: 0, "gpu.intel.com/tiles": 0}}

				mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(usedResources).Once()
				mockCache.On("AdjustPodResourcesL",
					mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
				mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(nodeTiles{}).Twice()
				mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(nodeTiles{}).Once()
				nodeNames := []string{nodename}
				args := extenderv1.ExtenderArgs{}
				args.NodeNames = &nodeNames
				args.Pod = pod

				result := gas.filterNodes(&args)
				So(result.Error, ShouldEqual, "")
				_, ok := result.FailedNodes[nodename]
				So(ok, ShouldEqual, testCase.expectedResult)
			}
		})

		iCache = origCacheAPI
	}
}

func TestFilterWithNContainerSameGPU(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := getFakePod(pluginResourceName)
		pod.Spec = *getMockPodSpecNCont(5, pluginResourceName)
		pod.Annotations[samegpuAnnotationName] = "container1,container2,container3,container4,container5"

		clientset := fake.NewSimpleClientset(pod)
		gas := NewGASExtender(clientset, false, false, "")

		mockCache := MockCacheAPI{}
		origCacheAPI := iCache
		iCache = &mockCache
		args := extenderv1.ExtenderBindingArgs{}
		args.Node = nodename

		type testCase struct {
			extraLabels    map[string]string
			description    string
			expectedResult bool
		}

		testCases := []testCase{
			{
				description:    "when there are 3 plugin resources left in cards, pod with 5 same-gpu containers should not fit",
				expectedResult: true,
			},
		}

		Convey("When node has 3 plugin resources left in cards, pod should not fit", t, func() {
			for _, testCase := range testCases {
				t.Logf("test %v", testCase.description)
				node := v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"gpu.intel.com/gpu-numbers": "0.1",
						},
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceName(pluginResourceName): resource.MustParse("16"),
							"gpu.intel.com/millicores":          resource.MustParse("2000"),
						},
						Allocatable: v1.ResourceList{
							v1.ResourceName(pluginResourceName): resource.MustParse("16"),
							"gpu.intel.com/millicores":          resource.MustParse("2000"),
						},
					},
				}
				for key, value := range testCase.extraLabels {
					node.Labels[key] = value
				}
				mockCache.On("FetchNode", mock.Anything, args.Node).Return(&node, nil).Once()

				usedResources := nodeResources{
					card0:   resourceMap{pluginResourceName: 5, "gpu.intel.com/millicores": 500},
					"card1": resourceMap{pluginResourceName: 5, "gpu.intel.com/millicores": 500},
				}

				mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(usedResources).Once()
				mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(nodeTiles{}).Once()
				nodeNames := []string{nodename}
				args := extenderv1.ExtenderArgs{}
				args.NodeNames = &nodeNames
				args.Pod = pod

				result := gas.filterNodes(&args)
				So(result.Error, ShouldEqual, "")
				_, ok := result.FailedNodes[nodename]
				So(ok, ShouldEqual, testCase.expectedResult)
			}
		})

		iCache = origCacheAPI
	}
}

func runSchedulingLogicWithMultiContainerXelinkedTileResourceReq(t *testing.T, pluginResourceName string) {
	t.Helper()

	ctx := context.TODO()
	origCacheAPI := iCache

	args := extenderv1.ExtenderBindingArgs{}
	args.Node = nodename

	type testCase struct {
		extraLabels            map[string]string
		extraAnnotations       map[string]string
		description            string
		expectedCardAnnotation string
		expectTimestamp        bool
		expectError            bool
		defaultTileCheck       bool
	}

	testCases := []testCase{
		{
			extraLabels: map[string]string{
				xeLinksLabel: "0.0-1.0_2.1-3.2",
			},
			extraAnnotations:       map[string]string{xelinkAnnotationName: trueValueString},
			description:            "4 card xe-linked success case",
			expectError:            false,
			expectedCardAnnotation: "card0,card1|card2,card3",
			expectTimestamp:        true,
			defaultTileCheck:       true,
		},
		{
			extraLabels: map[string]string{
				xeLinksLabel:       "0.0-1.0",
				xeLinksLabel + "2": "Z_2.1-3.2",
			},
			extraAnnotations:       map[string]string{xelinkAnnotationName: trueValueString},
			description:            "4 card xe-linked success case",
			expectError:            false,
			expectedCardAnnotation: "card0,card1|card2,card3",
			expectTimestamp:        true,
			defaultTileCheck:       true,
		},
		{
			extraLabels: map[string]string{
				xeLinksLabel:           "0.0-1.0_2.1-3.2",
				numaMappingLabel:       "0-0.1_1",
				numaMappingLabel + "2": "Z-2.3",
			},
			extraAnnotations: map[string]string{
				xelinkAnnotationName:     trueValueString,
				singleNumaAnnotationName: trueValueString,
			},
			description:            "4 card single-numa xe-linked success case",
			expectError:            false,
			expectedCardAnnotation: "card0,card1|card2,card3",
			expectTimestamp:        true,
			defaultTileCheck:       true,
		},
		{
			extraLabels: map[string]string{
				xeLinksLabel:     "0.0-2.0_2.1-3.2",
				numaMappingLabel: "0-0.1_1-2.3",
			},
			extraAnnotations: map[string]string{
				xelinkAnnotationName:     trueValueString,
				singleNumaAnnotationName: trueValueString,
			},
			description:            "4 card single-numa xe-linked fails if xe links span numa boundaries",
			expectError:            true,
			expectedCardAnnotation: "",
			expectTimestamp:        false,
			defaultTileCheck:       false,
		},
		{
			extraLabels: map[string]string{
				xeLinksLabel:     "0.0-2.0_2.1-3.2",
				numaMappingLabel: "0-0.1_1-2.3",
			},
			extraAnnotations: map[string]string{xelinkAnnotationName: trueValueString},
			description: "4 card single-numa xe-linked succeeds across numa boundaries " +
				"when single numa is not requested",
			expectError:            false,
			expectedCardAnnotation: "card0,card2|card2,card3",
			expectTimestamp:        true,
			defaultTileCheck:       true,
		},
	}

	Convey("When running scheduling logic with multi-container pod with tile request", t, func() {
		for _, testCase := range testCases {
			t.Logf("test %v", testCase.description)

			pod := getFakePod(pluginResourceName)
			mockNode := getMockNode(4, 4, pluginResourceName, card0, "card1", "card2", "card3")
			pod.Spec = *getMockPodSpecMultiContXeLinked(2, pluginResourceName)

			clientset := fake.NewSimpleClientset(pod)
			iCache = origCacheAPI
			gas := NewGASExtender(clientset, false, false, "")
			mockCache := MockCacheAPI{}
			iCache = &mockCache

			nodeRes := nodeResources{card0: resourceMap{pluginResourceName: 0, "gpu.intel.com/tiles": 0}}
			noTilesInUse := nodeTiles{card0: []int{}}

			for key, value := range testCase.extraLabels {
				mockNode.Labels[key] = value
			}

			for key, value := range testCase.extraAnnotations {
				pod.Annotations[key] = value
			}

			cardAnnotation := ""
			tileAnnotation := ""
			timestampFound := false
			applyCheck := func(action k8stesting.Action) (bool, runtime.Object, error) {
				patchAction, _ := action.(k8stesting.PatchAction)
				patch := patchAction.GetPatch()

				arr := []patchValue{}
				merr := json.Unmarshal(patch, &arr)
				if merr != nil {
					return false, nil, fmt.Errorf("error %w", merr)
				}

				for _, patch := range arr {
					switch {
					case strings.Contains(patch.Path, tsAnnotationName):
						timestampFound = true
					case strings.Contains(patch.Path, cardAnnotationName):
						cardAnnotation, _ = patch.Value.(string)
					case strings.Contains(patch.Path, tileAnnotationName):
						tileAnnotation, _ = patch.Value.(string)
					}
				}

				return true, nil, nil
			}

			mockCache.On("FetchNode", mock.Anything, mock.Anything).Return(mockNode, nil).Once()
			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeRes).Once()
			mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(noTilesInUse)
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(pod, nil).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything,
				mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

			clientset.Fake.PrependReactor("patch", "pods", applyCheck)
			result := gas.bindNode(ctx, &args)
			clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

			So(cardAnnotation, ShouldEqual, testCase.expectedCardAnnotation)
			if testCase.defaultTileCheck {
				split := strings.Split(tileAnnotation, "|")
				// Check the tile split between containers
				So(len(split), ShouldEqual, 2)
				So(strings.Count(split[0], "gt0"), ShouldEqual, 2)
				So(strings.Count(split[1], "card2:gt1"), ShouldEqual, 1)
				So(strings.Count(split[1], "card3:gt2"), ShouldEqual, 1)
			}

			So(timestampFound, ShouldEqual, testCase.expectTimestamp)
			if testCase.expectError {
				So(result.Error, ShouldNotEqual, "")
			} else {
				So(result.Error, ShouldEqual, "")
			}
		}
	})

	iCache = origCacheAPI
}

func TestRunSchedulingLogicWithMultiContainerXelinkedTileResourceReq(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		runSchedulingLogicWithMultiContainerXelinkedTileResourceReq(t, pluginResourceName)
	}
}

func TestRunSchedulingLogicWithMultiContainerTileResourceReq(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := getFakePod(pluginResourceName)

		clientset := fake.NewSimpleClientset(pod)
		gas := NewGASExtender(clientset, false, false, "tiles")
		mockNode := getMockNode(4, 4, pluginResourceName, card0)

		pod.Spec = *getMockPodSpecMultiCont(pluginResourceName)

		mockCache := MockCacheAPI{}
		origCacheAPI := iCache
		iCache = &mockCache

		args := extenderv1.ExtenderBindingArgs{}
		args.Node = nodename

		nodeRes := nodeResources{card0: resourceMap{pluginResourceName: 0, "gpu.intel.com/tiles": 0}}
		noTilesInUse := nodeTiles{card0: []int{}}

		ctx := context.TODO()

		Convey("When running scheduling logic with multi-container pod with tile request", t, func() {
			cardAnnotation := ""
			tileAnnotation := ""
			timestampFound := false
			applyCheck := func(action k8stesting.Action) (bool, runtime.Object, error) {
				patchAction, _ := action.(k8stesting.PatchAction)
				patch := patchAction.GetPatch()

				arr := []patchValue{}
				merr := json.Unmarshal(patch, &arr)
				if merr != nil {
					return false, nil, fmt.Errorf("error %w", merr)
				}

				for _, patch := range arr {
					switch {
					case strings.Contains(patch.Path, tsAnnotationName):
						timestampFound = true
					case strings.Contains(patch.Path, cardAnnotationName):
						cardAnnotation, _ = patch.Value.(string)
					case strings.Contains(patch.Path, tileAnnotationName):
						tileAnnotation, _ = patch.Value.(string)
					}
				}

				return true, nil, nil
			}

			mockCache.On("FetchNode", mock.Anything, mock.Anything).Return(mockNode, nil).Once()
			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeRes).Once()
			mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(noTilesInUse)
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(pod, nil).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything,
				mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

			clientset.Fake.PrependReactor("patch", "pods", applyCheck)
			result := gas.bindNode(ctx, &args)
			clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

			So(cardAnnotation, ShouldEqual, "card0|card0")
			split := strings.Split(tileAnnotation, "|")
			// Check the tile split between containers
			So(len(split), ShouldEqual, 2)
			So(strings.Count(split[0], "gt"), ShouldEqual, 3)
			So(strings.Count(split[1], "gt"), ShouldEqual, 1)
			// NOTE: tile annotation should include all the available tiles. If one or
			// more tiles are used twice then the tested code isn't working correctly
			So(strings.Contains(tileAnnotation, "gt0"), ShouldEqual, true)
			So(strings.Contains(tileAnnotation, "gt1"), ShouldEqual, true)
			So(strings.Contains(tileAnnotation, "gt2"), ShouldEqual, true)
			So(strings.Contains(tileAnnotation, "gt3"), ShouldEqual, true)

			So(timestampFound, ShouldEqual, true)
			So(result.Error, ShouldEqual, "")
		})

		iCache = origCacheAPI
	}
}

func TestTileDisablingDeschedulingAndPreference(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := getFakePod(pluginResourceName)

		clientset := fake.NewSimpleClientset(pod)
		gas := NewGASExtender(clientset, false, false, "")
		mockCache := MockCacheAPI{}
		origCacheAPI := iCache
		iCache = &mockCache
		args := extenderv1.ExtenderBindingArgs{}
		args.Node = nodename
		ctx := context.TODO()

		for _, labelPart := range []string{tileDisableLabelPrefix, tileDeschedLabelPrefix} {
			Convey("When node has a tile disabled/descheduled-label and the node card is in it", t, func() {
				mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
					Spec: *getMockPodSpecWithTile(1, pluginResourceName),
				}, nil).Once()
				mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"gpu.intel.com/cards":                       card0,
							"gpu.intel.com/tiles":                       "1",
							tasNSPrefix + policy + labelPart + card0Gt0: trueValueString,
							pciGroupLabel:                               "0",
						},
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceName(pluginResourceName): resource.MustParse("1"),
							"gpu.intel.com/tiles":               resource.MustParse("1"),
						},
						Allocatable: v1.ResourceList{
							v1.ResourceName(pluginResourceName): resource.MustParse("1"),
							"gpu.intel.com/tiles":               resource.MustParse("1"),
						},
					},
				}, nil).Once()
				mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
				mockCache.On("AdjustPodResourcesL",
					mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
				noTilesInUse := nodeTiles{card0: []int{}}
				mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(noTilesInUse).Once()

				result := gas.bindNode(ctx, &args)
				So(result.Error, ShouldEqual, "will not fit")
			})
		}

		Convey("When node has a tile descheduled label but another card to use", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				Spec: *getMockPodSpecWithTile(1, pluginResourceName),
			}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"gpu.intel.com/cards": "card0.card1",
						"gpu.intel.com/tiles": "2",
						tasNSPrefix + policy + tileDeschedLabelPrefix + "card1_gt0": trueValueString,
						pciGroupLabel: "0",
					},
				},
				Status: v1.NodeStatus{
					Capacity: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("2"),
						"gpu.intel.com/tiles":               resource.MustParse("2"),
					},
					Allocatable: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("2"),
						"gpu.intel.com/tiles":               resource.MustParse("2"),
					},
				},
			}, nil).Once()
			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
			noTilesInUse := nodeTiles{card0: []int{}, "card1": []int{}}
			mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(noTilesInUse).Twice()

			result := gas.bindNode(ctx, &args)
			So(result.Error, ShouldEqual, "")
		})

		Convey("When node has a preferred card label and fits", t, func() {
			applied := false
			applyCheck := func(action k8stesting.Action) (bool, runtime.Object, error) {
				patchAction, _ := action.(k8stesting.PatchAction)
				requiredStr := "card1"
				patch := patchAction.GetPatch()
				patchStr := string(patch)

				if !strings.Contains(patchStr, requiredStr) {
					return true, nil, errNotFound
				}

				applied = true

				return true, nil, nil
			}

			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				Spec: *getMockPodSpec(pluginResourceName),
			}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"gpu.intel.com/cards":                   "card0.card1",
						tasNSPrefix + policy + "gas-prefer-gpu": "card1",
						pciGroupLabel:                           "0_1",
					},
				},
				Status: v1.NodeStatus{
					Capacity:    v1.ResourceList{v1.ResourceName(pluginResourceName): resource.MustParse("2")},
					Allocatable: v1.ResourceList{v1.ResourceName(pluginResourceName): resource.MustParse("2")},
				},
			}, nil).Once()
			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
			noTilesInUse := nodeTiles{card0: []int{}, "card1": []int{}}
			mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(noTilesInUse).Once()

			clientset.Fake.PrependReactor("patch", "pods", applyCheck)
			result := gas.bindNode(ctx, &args)
			clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

			So(result.Error, ShouldEqual, "")
			So(applied, ShouldEqual, true)
		})

		Convey("When node has a tile preferred-label", t, func() {
			applied := false
			applyCheck := func(action k8stesting.Action) (bool, runtime.Object, error) {
				patchAction, _ := action.(k8stesting.PatchAction)
				requiredStr := "card0:gt3"
				patch := patchAction.GetPatch()
				patchStr := string(patch)

				if !strings.Contains(patchStr, requiredStr) {
					return true, nil, errNotFound
				}

				applied = true

				return true, nil, nil
			}

			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				Spec: *getMockPodSpecWithTile(1, pluginResourceName),
			}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"gpu.intel.com/cards": card0,
						"gpu.intel.com/tiles": "4",
						tasNSPrefix + policy + tileDisableLabelPrefix + card0Gt0: trueValueString,
						tasNSPrefix + policy + tilePrefLabelPrefix + card0:       "gt3",
						pciGroupLabel: "0",
					},
				},
				Status: v1.NodeStatus{
					Capacity: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/tiles":               resource.MustParse("4"),
					},
					Allocatable: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/tiles":               resource.MustParse("4"),
					},
				},
			}, nil).Once()
			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(nodeResources{}, nil).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()

			noTilesInUse := nodeTiles{card0: []int{}}
			mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(noTilesInUse).Twice()

			clientset.Fake.PrependReactor("patch", "pods", applyCheck)
			result := gas.bindNode(ctx, &args)
			clientset.Fake.ReactionChain = clientset.Fake.ReactionChain[1:]

			So(result.Error, ShouldEqual, "")
			So(applied, ShouldEqual, true)
		})

		iCache = origCacheAPI
	}
}

func TestTileSanitation(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := getFakePod(pluginResourceName)
		pod.Spec = *getMockPodSpecWithTile(1, pluginResourceName)

		clientset := fake.NewSimpleClientset(pod)
		gas := NewGASExtender(clientset, false, false, "")
		mockCache := MockCacheAPI{}
		origCacheAPI := iCache
		iCache = &mockCache
		args := extenderv1.ExtenderBindingArgs{}
		args.Node = nodename

		Convey("When node has an invalid tile disabled and pod should still fit", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				Spec: *getMockPodSpecWithTile(1, pluginResourceName),
			}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"gpu.intel.com/cards": card0,
						"gpu.intel.com/tiles": "1",
						tasNSPrefix + policy + tileDisableLabelPrefix + "card0_gt6": trueValueString,
						pciGroupLabel: "0",
					},
				},
				Status: v1.NodeStatus{
					Capacity: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/tiles":               resource.MustParse("1"),
					},
					Allocatable: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("1"),
						"gpu.intel.com/tiles":               resource.MustParse("1"),
					},
				},
			}, nil).Once()

			usedResources := nodeResources{card0: resourceMap{pluginResourceName: 0, "gpu.intel.com/tiles": 0}}

			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(usedResources).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
			mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(nodeTiles{}).Once()

			nodeNames := []string{nodename}
			args := extenderv1.ExtenderArgs{}
			args.NodeNames = &nodeNames
			args.Pod = pod

			result := gas.filterNodes(&args)
			So(result.Error, ShouldEqual, "")
			_, ok := result.FailedNodes[nodename]
			So(ok, ShouldEqual, false)
		})

		iCache = origCacheAPI
	}
}

func TestFilterWithDisabledTiles(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := getFakePod(pluginResourceName)
		pod.Spec = *getMockPodSpecWithTile(1, pluginResourceName)

		clientset := fake.NewSimpleClientset(pod)
		gas := NewGASExtender(clientset, false, false, "")
		mockCache := MockCacheAPI{}
		origCacheAPI := iCache
		iCache = &mockCache
		args := extenderv1.ExtenderBindingArgs{}
		args.Node = nodename

		Convey("When node has two cards and one disabled tile, pod should still fit", t, func() {
			mockCache.On("FetchPod", mock.Anything, args.PodNamespace, args.PodName).Return(&v1.Pod{
				Spec: *getMockPodSpecWithTile(1, pluginResourceName),
			}, nil).Once()
			mockCache.On("FetchNode", mock.Anything, args.Node).Return(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"gpu.intel.com/cards": "card0.card1",
						"gpu.intel.com/tiles": "2",
						tasNSPrefix + policy + tileDisableLabelPrefix + "card1_gt0": trueValueString,
					},
				},
				Status: v1.NodeStatus{
					Capacity: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("2"),
						"gpu.intel.com/tiles":               resource.MustParse("2"),
					},
					Allocatable: v1.ResourceList{
						v1.ResourceName(pluginResourceName): resource.MustParse("2"),
						"gpu.intel.com/tiles":               resource.MustParse("2"),
					},
				},
			}, nil).Once()

			usedResources := nodeResources{card0: resourceMap{pluginResourceName: 0, "gpu.intel.com/tiles": 0}}

			mockCache.On("GetNodeResourceStatus", mock.Anything, mock.Anything).Return(usedResources).Once()
			mockCache.On("AdjustPodResourcesL",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
			mockCache.On("GetNodeTileStatus", mock.Anything, mock.Anything).Return(nodeTiles{}).Twice()
			nodeNames := []string{nodename}
			args := extenderv1.ExtenderArgs{}
			args.NodeNames = &nodeNames
			args.Pod = pod

			result := gas.filterNodes(&args)
			So(result.Error, ShouldEqual, "")
			_, ok := result.FailedNodes[nodename]
			So(ok, ShouldEqual, false)
		})

		iCache = origCacheAPI
	}
}

func TestSanitizeSamegpulist(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
			Spec: *getMockPodSpecMultiContSamegpu(pluginResourceName),
		}

		wrongValueReason := map[string]string{
			"container1,container5":            "Listing absent containers makes gas-same-gpu ignored",
			"container1":                       "Listing single container name makes gas-same-gpu ignored",
			"":                                 "Empty gas-same-gpu annotation is ignored",
			"container1,container2,container2": "gas-same-gpu with duplicates is ignored",
		}

		Convey("Ensure no gas-same-gpu annotation returns blank list with no error",
			t, func() {
				containerNames, err := containersRequestingSamegpu(pod)
				So(len(containerNames), ShouldEqual, 0)
				So(err, ShouldEqual, nil)
			})

		for wrongValue, reason := range wrongValueReason {
			pod.ObjectMeta.Annotations["gas-same-gpu"] = wrongValue

			Convey(reason,
				t, func() {
					containerNames, err := containersRequestingSamegpu(pod)
					So(len(containerNames), ShouldEqual, 0)
					So("malformed annotation", ShouldEqual, err.Error())
				})
		}

		pod.ObjectMeta.Annotations["gas-same-gpu"] = "container2,container3"

		Convey("Ensure correct annotation returns all listed container names with no error",
			t, func() {
				containerNames, err := containersRequestingSamegpu(pod)
				So(containerNames, ShouldResemble, map[string]bool{"container2": true, "container3": true})
				So(err, ShouldEqual, nil)
			})
	}
}

func TestSanitizeSamegpuResourcesRequest(t *testing.T) {
	for _, pluginResourceName := range []string{i915PluginResource, xePluginResource} {
		for _, monitoringResourceName := range []string{xeMonitoringResource, i915MonitoringResource} {
			Convey("Tiles and monitoring resources are not allowed in same-gpu resourceRequests",
				t, func() {
					// fail because of tiles
					samegpuIndexes := map[int]bool{0: true}
					resourceRequests := []resourceMap{
						{pluginResourceName: 1, "gpu.intel.com/tiles": 2},
					}
					err := sanitizeSamegpuResourcesRequest(samegpuIndexes, resourceRequests)
					So(err.Error(), ShouldEqual, "resources conflict")

					// fail because of monitoring
					samegpuIndexes = map[int]bool{0: true}
					resourceRequests = []resourceMap{
						{pluginResourceName: 1, monitoringResourceName: 1},
					}
					err = sanitizeSamegpuResourcesRequest(samegpuIndexes, resourceRequests)
					So(err.Error(), ShouldEqual, "resources conflict")

					// success
					samegpuIndexes = map[int]bool{0: true}
					resourceRequests = []resourceMap{
						{
							pluginResourceName:         1,
							"gpu.intel.com/millicores": 100,
							"gpu.intel.com/memory.max": 8589934592,
						},
					}
					err = sanitizeSamegpuResourcesRequest(samegpuIndexes, resourceRequests)
					So(err, ShouldEqual, nil)
				})

			Convey("Same-gpu containers should have exactly one device resource requested",
				t, func() {
					// failure heterogeneous
					samegpuIndexes := map[int]bool{0: true, 1: true}
					resourceRequests := []resourceMap{
						{pluginResourceName: 1, "gpu.intel.com/millicores": 200},
						{pluginResourceName: 2, "gpu.intel.com/millicores": 200},
					}
					err := sanitizeSamegpuResourcesRequest(samegpuIndexes, resourceRequests)
					So(err.Error(), ShouldEqual, "resources conflict")

					// Failure homogeneous
					samegpuIndexes = map[int]bool{0: true, 1: true}
					resourceRequests = []resourceMap{
						{pluginResourceName: 2, "gpu.intel.com/millicores": 200},
						{pluginResourceName: 2, "gpu.intel.com/millicores": 200},
					}
					err = sanitizeSamegpuResourcesRequest(samegpuIndexes, resourceRequests)
					So(err.Error(), ShouldEqual, "resources conflict")

					// Success
					samegpuIndexes = map[int]bool{0: true, 1: true}
					resourceRequests = []resourceMap{
						{pluginResourceName: 1, "gpu.intel.com/millicores": 200},
						{pluginResourceName: 1, "gpu.intel.com/millicores": 300},
					}
					err = sanitizeSamegpuResourcesRequest(samegpuIndexes, resourceRequests)
					So(err, ShouldEqual, nil)
				})
		}
	}
}
