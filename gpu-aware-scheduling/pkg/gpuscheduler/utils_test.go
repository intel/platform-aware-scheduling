//go:build !validation
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
	Convey("When the GPU belongs to a PCI Group", t, func() {
		node := getMockNode(1, 1)
		node.Labels[pciGroupLabel] = "0.1_2.3.4"
		So(getPCIGroup(node, "card0"), ShouldResemble, []string{"0", "1"})
		So(getPCIGroup(node, "card1"), ShouldResemble, []string{"0", "1"})
		So(getPCIGroup(node, "card2"), ShouldResemble, []string{"2", "3", "4"})
		So(getPCIGroup(node, "card3"), ShouldResemble, []string{"2", "3", "4"})
		So(getPCIGroup(node, "card4"), ShouldResemble, []string{"2", "3", "4"})
	})

	Convey("When the GPU belongs to a PCI Group with multiple group labels", t, func() {
		node := getMockNode(1, 1)
		node.Labels[pciGroupLabel] = "0.1_2.3.4_"
		node.Labels[pciGroupLabel+"2"] = "5.6_7.8_11.12_"
		node.Labels[pciGroupLabel+"3"] = "9.10"
		So(getPCIGroup(node, "card6"), ShouldResemble, []string{"5", "6"})
		So(getPCIGroup(node, "card9"), ShouldResemble, []string{"9", "10"})
		So(getPCIGroup(node, "card20"), ShouldResemble, []string{})
	})

	Convey("When I call addPCIGroupGPUs with a proper node and cards map", t, func() {
		node := getMockNode(1, 1)
		node.Labels[pciGroupLabel] = "0.1_2.3.4"
		cards := []string{}
		cards = addPCIGroupGPUs(node, "card3", cards)

		So(len(cards), ShouldEqual, 3)
		So(cards, ShouldContain, "card2")
		So(cards, ShouldContain, "card3")
		So(cards, ShouldContain, "card4")

		cards2 := []string{}
		cards2 = addPCIGroupGPUs(node, "card0", cards2)

		So(len(cards2), ShouldEqual, 2)
		So(cards2, ShouldContain, "card0")
		So(cards2, ShouldContain, "card1")
	})
}

func TestTASNamespaceStrip(t *testing.T) {
	Convey("When proper label with tas namespace is given", t, func() {
		without, status := labelWithoutTASNS("telemetry.aware.scheduling.foobar/gas-disable-card0")

		So(without, ShouldEqual, "gas-disable-card0")
		So(status, ShouldEqual, true)
	})
	Convey("When bad label without tas namespace is given", t, func() {
		_, status := labelWithoutTASNS("tellemetry.aware.scheduling.foobar/gas-disable-card0")

		So(status, ShouldEqual, false)
	})
}

func TestCreateTileMapping(t *testing.T) {
	Convey("When proper set of gas labels are processed", t, func() {
		labels := make(map[string]string)
		labels["telemetry.aware.scheduling.foobar/gas-tile-disable-card5_gt99"] = trueValueString
		labels["telemetry.aware.scheduling.foobar/gas-tile-deschedule-card2_gt2"] = trueValueString
		labels["telemetry.aware.scheduling.foobar/gas-tile-deschedule-card2_gt3"] = trueValueString
		labels["telemetry.aware.scheduling.foobar/gas-tile-preferred-card2"] = "gt1"

		dis, des, pref := createTileMapping(labels)

		So(len(dis), ShouldEqual, 1)
		So(len(des), ShouldEqual, 1)
		So(len(pref), ShouldEqual, 1)

		So(len(pref["card5"]), ShouldEqual, 0)
		So(len(dis["card5"]), ShouldEqual, 1)
		So(len(des["card5"]), ShouldEqual, 0)
		So(len(dis["card2"]), ShouldEqual, 0)
		So(len(des["card2"]), ShouldEqual, 2)
		So(len(pref["card2"]), ShouldEqual, 1)

		So(dis["card5"], ShouldContain, 99)

		So(des["card2"], ShouldContain, 2)
		So(des["card2"], ShouldContain, 3)

		So(pref["card2"], ShouldContain, 1)
	})

	Convey("When bad set of gas labels are processed", t, func() {
		labels := make(map[string]string)
		labels["telemetry.aware.scheduling.foobar/gas-tile-disable-cardX_gt3"] = trueValueString
		labels["telemetry.aware.scheduling.foobar/gas-tile-deschedule-card2_gtRRrr"] = trueValueString
		labels["telemetry.aware.scheduling.foobar/gas-tile-deschedule-carrd2_gt3"] = trueValueString
		labels["telemetry.aware.scheduling.foobar/gas-tile-preferred-card2"] = "gx1"

		dis, des, pref := createTileMapping(labels)

		So(len(dis), ShouldEqual, 0)
		So(len(des), ShouldEqual, 0)
		So(len(pref), ShouldEqual, 0)
	})
}

func TestCreateDisabledTileMapping(t *testing.T) {
	Convey("When proper set of gas labels are processed", t, func() {
		labels := make(map[string]string)
		labels["telemetry.aware.scheduling.foobar/gas-tile-disable-card5_gt99"] = trueValueString
		labels["telemetry.aware.scheduling.foobar/gas-tile-deschedule-card2_gt2"] = trueValueString
		labels["telemetry.aware.scheduling.foobar/gas-tile-deschedule-card2_gt3"] = trueValueString
		labels["telemetry.aware.scheduling.foobar/gas-tile-disable-card2_gt6"] = trueValueString
		labels["telemetry.aware.scheduling.foobar/gas-tile-preferred-card2"] = "gt1"

		dis := createDisabledTileMapping(labels)

		So(len(dis), ShouldEqual, 2)

		So(len(dis["card5"]), ShouldEqual, 1)
		So(len(dis["card2"]), ShouldEqual, 3)

		So(dis["card5"], ShouldContain, 99)

		So(dis["card2"], ShouldContain, 2)
		So(dis["card2"], ShouldContain, 3)
		So(dis["card2"], ShouldContain, 6)
		So(dis["card2"], ShouldNotContain, 1)
	})
}

func TestReorderTiles(t *testing.T) {
	Convey("When reordering with one preferred tile", t, func() {
		tiles := []int{1, 2, 3, 4}
		prefs := []int{3}

		tiles = reorderPreferredTilesFirst(tiles, prefs)

		So(len(tiles), ShouldEqual, 4)
		So(tiles, ShouldResemble, []int{3, 2, 1, 4})
	})

	Convey("When reordering with two preferred tile", t, func() {
		tiles := []int{1, 2, 3, 4}
		prefs := []int{3, 1}

		tiles = reorderPreferredTilesFirst(tiles, prefs)

		So(len(tiles), ShouldEqual, 4)
		So(tiles, ShouldResemble, []int{3, 1, 2, 4})
	})

	Convey("When reordering with three preferred tile", t, func() {
		tiles := []int{1, 2, 3, 4}
		prefs := []int{3, 1, 4}

		tiles = reorderPreferredTilesFirst(tiles, prefs)

		So(len(tiles), ShouldEqual, 4)
		So(tiles, ShouldResemble, []int{3, 1, 4, 2})
	})

	Convey("When reordering with no preferred tile", t, func() {
		tiles := []int{1, 2, 3, 4}
		prefs := []int{}

		tiles = reorderPreferredTilesFirst(tiles, prefs)

		So(len(tiles), ShouldEqual, 4)
		So(tiles, ShouldResemble, []int{1, 2, 3, 4})
	})

	Convey("When reordering with invalid preferred tile", t, func() {
		tiles := []int{1, 2, 3, 4}
		prefs := []int{6, 7, 9}

		tiles = reorderPreferredTilesFirst(tiles, prefs)

		So(len(tiles), ShouldEqual, 4)
		So(tiles, ShouldResemble, []int{1, 2, 3, 4})
	})
}

func TestConvertPodTileAnnotationToCardTileCombos(t *testing.T) {
	Convey("When converting a valid annotation", t, func() {
		anno := "card0:gt1+gt4|card1:gt2||card4:gt0,card6:gt99"

		combos := convertPodTileAnnotationToCardTileMap(anno)

		So(len(combos), ShouldEqual, 5)
		So(combos, ShouldResemble, map[string]bool{"0.1": true, "0.4": true, "1.2": true, "4.0": true, "6.99": true})
	})

	Convey("When converting an invalid annotation", t, func() {
		anno := "card0:gt1Xgt4|cardy:gt2||card4Zgt0,card6:gt9x9"

		combos := convertPodTileAnnotationToCardTileMap(anno)

		So(len(combos), ShouldEqual, 0)
	})
}

func TestSanitizeTiles(t *testing.T) {
	disabled := DisabledTilesMap{"card0": {0, 3, 4}, "card1": {8, 9}}

	Convey("When sanitizing tiles", t, func() {
		disabled = sanitizeTiles(disabled, 4)
		So(disabled["card0"], ShouldResemble, []int{0, 3})
		So(disabled["card1"], ShouldResemble, []int{})
	})
}

func TestConcatenateSplitLabel(t *testing.T) {
	Convey("When the label is split, it can be concatenated", t, func() {
		node := getMockNode(1, 1)
		node.Labels[pciGroupLabel] = "foo"
		node.Labels[pciGroupLabel+"2"] = "bar"
		node.Labels[pciGroupLabel+"3"] = "ber"
		result := concatenateSplitLabel(node, pciGroupLabel)
		So(result, ShouldEqual, "foobarber")
	})
}

func TestContainerRequestsNoSamegpu(t *testing.T) {
	Convey(
		"With empty same-gpu list, empty map and a full list of resource requests is expected",
		t, func() {
			pod := &v1.Pod{
				Spec: *getMockPodSpecMultiContSamegpu(),
			}
			samegpuSearchmap, allResourceRequests := containerRequests(pod, map[string]bool{})
			So(len(samegpuSearchmap), ShouldEqual, 0)
			So(len(allResourceRequests), ShouldEqual, len(pod.Spec.Containers))
		})
	Convey(
		"With same-gpu list, map of respective indexes should be returned and full list of resource requests",
		t, func() {
			pod := &v1.Pod{
				Spec: *getMockPodSpecMultiContSamegpu(),
			}
			samegpuNames := map[string]bool{"container2": true, "container3": true}
			samegpuSearchmap, allRequests := containerRequests(pod, samegpuNames)
			So(len(samegpuSearchmap), ShouldEqual, len(samegpuNames))
			So(len(allRequests), ShouldEqual, len(pod.Spec.Containers))
			So(samegpuSearchmap, ShouldResemble, map[int]bool{1: true, 2: true})
		})
}
