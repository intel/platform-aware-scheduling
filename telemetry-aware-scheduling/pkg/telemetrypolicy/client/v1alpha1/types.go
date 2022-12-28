// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package client telemetrypolicy/api/client provides an interface to interact with Policy CRD through a custom Client.
package client

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

// Client holds the information needed to query telemetry policies from the kubernetes API.
type Client struct {
	parameterCodec runtime.ParameterCodec
	rest           *rest.RESTClient
	namespace      string
	plural         string
}
