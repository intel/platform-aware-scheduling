// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package gpuscheduler

import (
	"errors"

	"k8s.io/klog/v2"
)

const (
	minAllowedInput = 0
)

// Errors.
var (
	errOverflow = errors.New("integer overflow")
	errInput    = errors.New("input error")
)

// Map of resources. name -> resource amount.
type resourceMap map[string]int64

func (rm resourceMap) newCopy() resourceMap {
	mapCopy := make(resourceMap, len(rm))

	mapCopy.copyFrom(rm)

	return mapCopy
}

func (rm resourceMap) copyFrom(src resourceMap) {
	for key := range src {
		rm[key] = src[key]
	}
}

// addRM adds the resources of the src resourceMap and returns nil.
// If there is an error adding the resources, nothing will be added.
func (rm resourceMap) addRM(src resourceMap) error {
	mapCopy := rm.newCopy()
	// check with the copy that the src fits
	for key, value := range src {
		err := mapCopy.add(key, value)
		if err != nil {
			klog.Error("addRM failed")

			return err
		}
	}

	rm.copyFrom(mapCopy)

	return nil
}

// subtractRM removes the resources of the src resourceMap and returns nil.
// If there is an error removing the resources, nothing will be removed.
// If any resource amount would go negative, it is set to zero.
func (rm resourceMap) subtractRM(src resourceMap) error {
	mapCopy := rm.newCopy()
	// check with the copy that the src can be subtracted
	for key, value := range src {
		err := mapCopy.subtract(key, value)
		if err != nil {
			klog.Error("subtractRM failed")

			return err
		}
	}

	rm.copyFrom(mapCopy)

	return nil
}

// add adds a resource to the map.
// It assumes being used on a sane resource map with positive values.
func (rm resourceMap) add(key string, value int64) error {
	if value < minAllowedInput {
		klog.Error("bad input for add, key:", key)

		return errInput
	}

	oldVal, ok := rm[key]
	if ok {
		value += oldVal

		if value < 0 {
			klog.Error("overflow during add, key:", key)

			return errOverflow
		}
	}

	rm[key] = value

	return nil
}

// subtract removes a resource amount from the map.
// It assumes being used on a sane resource map with positive values.
// If the resource amount would go negative, it is set to zero.
func (rm resourceMap) subtract(key string, value int64) error {
	if value < minAllowedInput {
		klog.Error("bad input for subtract, key:", key)

		return errInput
	}

	oldVal, ok := rm[key]
	if ok {
		rm[key] = oldVal - value

		if rm[key] < 0 {
			// for sake of robustness, try to return to sane resource amount of zero, with a warning
			klog.Warningf("resource value for %v ended negative, capped to zero", key)

			rm[key] = 0
		}
	} else {
		klog.Error("subtract attempted with non-existing key:", key)

		return errInput
	}

	return nil
}

func (rm resourceMap) divide(divider int) error {
	if divider < 1 {
		klog.Error("bad divider")

		return errInput
	}

	if divider == 1 {
		return nil
	}

	for key, value := range rm {
		rm[key] = value / int64(divider)
	}

	return nil
}
