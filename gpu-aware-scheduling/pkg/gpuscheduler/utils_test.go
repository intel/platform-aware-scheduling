// +build !validation

// nolint:testpackage
package gpuscheduler

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	v1 "k8s.io/api/core/v1"
)

func TestHasGPUResources(t *testing.T) {
	Convey("When I check if a nil pod has gpu resources", t, func() {
		result := hasGPUResources(nil)
		So(result, ShouldEqual, false)
	})
}

func TestIsCompletePod(t *testing.T) {
	Convey("When I check if a succeeded pod is complete", t, func() {
		pod := v1.Pod{}
		pod.Status.Phase = v1.PodSucceeded
		result := isCompletedPod(&pod)
		So(result, ShouldEqual, true)
	})
}
