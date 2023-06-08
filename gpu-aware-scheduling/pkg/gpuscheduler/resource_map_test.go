// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

//go:build !validation
// +build !validation

//nolint:testpackage
package gpuscheduler

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

const (
	key = "foo"
)

func TestDivision(t *testing.T) {
	resMap := resourceMap{key: 2}

	Convey("When I divide a rm with -1", t, func() {
		err := resMap.divide(-1)
		So(resMap[key], ShouldEqual, 2)
		So(err, ShouldNotBeNil)
	})
	Convey("When I divide a rm with 1", t, func() {
		err := resMap.divide(1)
		So(resMap[key], ShouldEqual, 2)
		So(err, ShouldBeNil)
	})
	Convey("When I divide a rm with 2", t, func() {
		err := resMap.divide(2)
		So(resMap[key], ShouldEqual, 1)
		So(err, ShouldBeNil)
	})
}

func TestAdd(t *testing.T) {
	int64Max := int64(9223372036854775807)
	resMap := resourceMap{key: 2}

	Convey("When I add to RM 9223372036854775805", t, func() {
		err := resMap.add(key, (int64Max - 2))
		So(resMap[key], ShouldEqual, int64Max)
		So(err, ShouldBeNil)
	})
	Convey("When I still add to RM 1", t, func() {
		err := resMap.add(key, 1)
		So(resMap[key], ShouldEqual, int64Max)
		So(err, ShouldEqual, errOverflow)
	})
}

func TestSubtract(t *testing.T) {
	resMap := resourceMap{key: 2}

	Convey("When I subtract an unknown key from RM", t, func() {
		err := resMap.subtract("bar", 2)
		So(resMap[key], ShouldEqual, 2)
		So(err, ShouldNotBeNil)
	})
	Convey("When I subtract 1 from RM", t, func() {
		err := resMap.subtract(key, 1)
		So(resMap[key], ShouldEqual, 1)
		So(err, ShouldBeNil)
	})
	Convey("When I again subtract 2 from RM", t, func() {
		err := resMap.subtract(key, 2)
		So(resMap[key], ShouldEqual, 0)
		So(err, ShouldBeNil)
	})
}

func TestAddRM(t *testing.T) {
	int64Max := int64(9223372036854775807)
	key2 := "foo2"
	key3 := "foo3"
	rm1 := resourceMap{key: 2, key2: 3}
	rm2 := resourceMap{key: 4, key2: 5, key3: int64Max}
	rm3 := resourceMap{key: 2, key2: 3, key3: int64Max}

	Convey("When I add RM to another RM which overflows", t, func() {
		err := rm2.addRM(rm3)
		So(rm2[key], ShouldEqual, 4)
		So(rm2[key2], ShouldEqual, 5)
		So(rm2[key3], ShouldEqual, int64Max)
		So(err, ShouldEqual, errOverflow)
	})
	Convey("When I add RM to another RM which fits", t, func() {
		err := rm1.addRM(rm2)
		So(rm1[key], ShouldEqual, 6)
		So(rm1[key2], ShouldEqual, 8)
		So(rm1[key3], ShouldEqual, int64Max)
		So(err, ShouldBeNil)
	})
}

func TestSubtractRM(t *testing.T) {
	int64Max := int64(9223372036854775807)
	key2 := "foo2"
	key3 := "foo3"
	rm := resourceMap{"unknown": 2, key2: 3}
	rm2 := resourceMap{key: 4, key2: 5, key3: int64Max}
	rm3 := resourceMap{key: 2, key2: 3, key3: int64Max}

	Convey("When I subtract an RM with an unknown key from another RM", t, func() {
		err := rm2.subtractRM(rm)
		So(rm2[key], ShouldEqual, 4)
		So(rm2[key2], ShouldEqual, 5)
		So(rm2[key3], ShouldEqual, int64Max)
		So(err, ShouldNotBeNil)
	})
	Convey("When I subtract RM from another RM", t, func() {
		err := rm2.subtractRM(rm3)
		So(rm2[key], ShouldEqual, 2)
		So(rm2[key2], ShouldEqual, 2)
		So(rm2[key3], ShouldEqual, 0)
		So(err, ShouldBeNil)
	})
}
