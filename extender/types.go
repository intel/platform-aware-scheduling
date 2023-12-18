// Copyright (C) 2022 Intel Corporation
// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package extender

import (
	"net/http"
)

// Scheduler has the capabilities needed to prioritize and filter nodes based on http requests.
type Scheduler interface {
	Bind(w http.ResponseWriter, r *http.Request)
	Prioritize(w http.ResponseWriter, r *http.Request)
	Filter(w http.ResponseWriter, r *http.Request)
}

// Server type wraps the implementation of the extender.
type Server struct {
	Scheduler
}
