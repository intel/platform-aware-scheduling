// +build !validation

// nolint:testpackage
package gpuscheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/intel/platform-aware-scheduling/extender"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func getDummyExtender() *GASExtender {
	var nilRestConfig *rest.Config

	mockClientAPI := new(MockClientAPI)
	iClient = mockClientAPI

	mockClientAPI.On("InClusterConfig").Return(nil, nil)
	mockClientAPI.On("NewForConfig", nilRestConfig).Return(nil, nil)

	clientset := fake.NewSimpleClientset()

	return NewGASExtender(clientset)
}

func TestRealGetPod(t *testing.T) {
	// this is a useless test, but the real get pod only seldom gets
	// called since it is found only in an error path
	Convey("When I call GetPod on a fake client", t, func() {
		clientset := fake.NewSimpleClientset()
		pod, err := iClient.GetPod(clientset, "foo", "bar")
		So(pod, ShouldBeNil)
		So(err, ShouldNotBeNil)
	})
}

func TestNewGASExtender(t *testing.T) {
	Convey("When I create a new gas extender", t, func() {
		mockClientAPI := new(MockClientAPI)
		iClient = mockClientAPI
		Convey("and InClusterConfig returns an error", func() {
			mockClientAPI.On("InClusterConfig").Return(nil, errMock)
			gas := NewGASExtender(nil)
			So(gas.clientset, ShouldBeNil)
		})

		Convey("and InClusterConfig returns a nil config without error", func() {
			mockClientAPI.On("InClusterConfig").Return(nil, nil)
			var nilRestConfig *rest.Config
			mockClientAPI.On("NewForConfig", nilRestConfig).Return(nil, errMock)
			gas := NewGASExtender(nil)
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
		result, err := gas.runSchedulingLogic(&pod, "")
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

	args.NodeNames = &[]string{"nodename"}
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
