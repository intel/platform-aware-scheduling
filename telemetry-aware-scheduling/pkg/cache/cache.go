// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package cache

type requestType uint

// const for requestType value.
const (
	READ requestType = iota
	WRITE
	DELETE
)

// Interface contains the baseline behaviour for a cache.
type Interface interface {
	add(key string, payload interface{})
	delete(key string)
	read(key string) interface{}
}

// concurrentCache is a type that holds information only accessible through a channel making it concurrent safe.
type concurrentCache struct {
	cache chan request
}

// request is an information structure sent to the cache in order to run and retrieve objects.
type request struct {
	Type    requestType
	Payload interface{}
	Key     string
	Out     chan request
}

// newRequest returns a request which is properly initialized.
func (c concurrentCache) newRequest(t requestType) request {
	switch t {
	case READ:
		return request{Type: t, Out: make(chan request)}
	default:
		return request{Type: t}
	}
}

// run executes a constant goroutine waiting for requests and setting up the initial data structure.
func (c concurrentCache) run(ch chan request, initialData map[string]interface{}) {
	cache := initialData

	for {
		req := <-ch
		switch req.Type {
		case READ:
			req.Payload = cache[req.Key]
			go sendToChannel(req.Out, req)
		case WRITE:
			// payload isn't changed if nil. This allows writing the same metric name without deleting current information.
			if req.Payload == nil {
				if v, ok := cache[req.Key]; ok {
					req.Payload = v
				}
			}

			cache[req.Key] = req.Payload
		case DELETE:
			delete(cache, req.Key)
		}
	}
}

// sendToChannel is a helper method that sends the request to the passed channel.
func sendToChannel(ch chan request, req request) {
	ch <- req
}

// add creates a write request and sends it into the cache channel.
func (c concurrentCache) add(key string, payload interface{}) {
	r := c.newRequest(WRITE)
	r.Key = key
	r.Payload = payload
	c.cache <- r
	sendToChannel(c.cache, r)
}

// delete removes the passed metric name from the cache with associate metric info.
func (c concurrentCache) delete(key string) {
	r := c.newRequest(DELETE)
	r.Key = key
	sendToChannel(c.cache, r)
}

// read creates a read request and sends it into the cache channel.
func (c concurrentCache) read(itemName string) interface{} {
	r := c.newRequest(READ)
	r.Key = itemName
	sendToChannel(c.cache, r)
	info := <-r.Out
	data := info.Payload

	return data
}
