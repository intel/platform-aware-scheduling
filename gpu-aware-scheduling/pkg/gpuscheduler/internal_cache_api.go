// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package gpuscheduler

import "k8s.io/client-go/tools/cache"

type internalCacheAPI struct{}

func (r *internalCacheAPI) WaitForCacheSync(stopCh <-chan struct{}, cacheSyncs ...cache.InformerSynced) bool {
	return cache.WaitForCacheSync(stopCh, cacheSyncs...)
}
