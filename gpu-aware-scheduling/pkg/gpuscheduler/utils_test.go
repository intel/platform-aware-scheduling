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

func TestPCIGroups(t *testing.T) {
	Convey("When there GPU belongs to a PCI Group", t, func() {
		node := getMockNode()
		node.Labels[pciGroupLabel] = "0.1_2.3.4"
		So(getPCIGroup(node, "card0"), ShouldResemble, []string{"0", "1"})
		So(getPCIGroup(node, "card1"), ShouldResemble, []string{"0", "1"})
		So(getPCIGroup(node, "card2"), ShouldResemble, []string{"2", "3", "4"})
		So(getPCIGroup(node, "card3"), ShouldResemble, []string{"2", "3", "4"})
		So(getPCIGroup(node, "card4"), ShouldResemble, []string{"2", "3", "4"})
	})

	Convey("When I call addPCIGroupGPUs with a proper node and cards map", t, func() {
		node := getMockNode()
		node.Labels[pciGroupLabel] = "0.1_2.3.4"
		cards := map[string]string{"card3": pciGroupValue}
		addPCIGroupGPUs(node, cards)
		So(len(cards), ShouldEqual, 3)
		_, ok := cards["card0"]
		So(ok, ShouldBeFalse)
		_, ok = cards["card1"]
		So(ok, ShouldBeFalse)
		value, ok := cards["card2"]
		So(ok, ShouldBeTrue)
		So(value, ShouldEqual, "grouped")
		value, ok = cards["card4"]
		So(ok, ShouldBeTrue)
		So(value, ShouldEqual, "grouped")
		value, ok = cards["card3"]
		So(ok, ShouldBeTrue)
		So(value, ShouldEqual, pciGroupValue)
	})
}
