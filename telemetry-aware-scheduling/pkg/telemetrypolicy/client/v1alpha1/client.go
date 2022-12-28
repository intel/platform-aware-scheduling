// Copyright (C) 2022 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"fmt"

	telemetrypolicy "github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

// NewRest returns a Kubernetes Rest client to access the Telemetry Policy CRD.
func NewRest(config rest.Config) (*rest.RESTClient, *runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	schemeInfo := crdScheme()

	if err := schemeInfo.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	config.GroupVersion = &schemeInfo.SchemeGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.NewCodecFactory(scheme).WithoutConversion()

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get rest client: %w", err)
	}

	return client, scheme, nil
}

// New returns a rest client that specifically returns a namespaced client to retrieve Telemetry Policy from the API.
func New(config rest.Config, namespace string) (*Client, error) {
	rest, scheme, err := NewRest(config)

	if err != nil {
		return nil, err
	}

	return &Client{
			runtime.NewParameterCodec(scheme),
			rest,
			namespace,
			telemetrypolicy.Plural,
		},
		nil
}

// Create sends the given object to the API server to register it as a new Telemetry Policy.
func (client *Client) Create(obj *telemetrypolicy.TASPolicy) (*telemetrypolicy.TASPolicy, error) {
	var result telemetrypolicy.TASPolicy

	err := client.rest.Post().Namespace(obj.Namespace).Resource(client.plural).Body(obj).Do(context.TODO()).Into(&result)
	if err != nil {
		return &result, fmt.Errorf("failed to register the policy: %w", err)
	}

	return &result, nil
}

// Update changes the information contained in a given Telemetry Policy.
func (client *Client) Update(obj *telemetrypolicy.TASPolicy) (*telemetrypolicy.TASPolicy, error) {
	var result telemetrypolicy.TASPolicy

	err := client.rest.Put().Namespace(obj.Namespace).Resource(client.plural).Body(obj).Name(obj.Name).Do(context.TODO()).Into(&result)
	if err != nil {
		return &result, fmt.Errorf("failed to update the policy: %w", err)
	}

	return &result, nil
}

// Get returns the full information from the named Telemetry Policy.
func (client *Client) Get(name string, namespace string) (*telemetrypolicy.TASPolicy, error) {
	var result telemetrypolicy.TASPolicy

	err := client.rest.Get().Namespace(namespace).Resource(client.plural).Name(name).Do(context.TODO()).Into(&result)
	if err != nil {
		return &result, fmt.Errorf("failed to get the policy: %w", err)
	}

	return &result, nil
}

// Delete removes a telemetry policy of the given name, with the passed options, from Kubernetes.
func (client *Client) Delete(name string, options *metav1.DeleteOptions) error {
	err := client.rest.Delete().Namespace(client.namespace).Resource(client.plural).Name(name).Body(options).Do(context.TODO()).Error()
	if err != nil {
		return fmt.Errorf("failed to delete the policy: %w", err)
	}

	return nil
}

// List returns a list of Telemetry Policy that meet the conditions set forward in the options argument.
func (client *Client) List(options metav1.ListOptions) (*telemetrypolicy.TASPolicyList, error) {
	var result telemetrypolicy.TASPolicyList

	err := client.rest.Get().Namespace(client.namespace).Resource(client.plural).VersionedParams(&options, client.parameterCodec).Do(context.TODO()).Into(&result)
	if err != nil {
		return &result, fmt.Errorf("failed to list policies: %w", err)
	}

	return &result, nil
}

// NewListWatch creates a watcher on the CRD.
func (client *Client) NewListWatch() *cache.ListWatch {
	return cache.NewListWatchFromClient(client.rest, client.plural, client.namespace, fields.Everything())
}

// groupversion gives access to the Group Version struct for the API.
func groupVersion() schema.GroupVersion {
	return schema.GroupVersion{
		Group:   telemetrypolicy.Group,
		Version: telemetrypolicy.Version,
	}
}

// schemeInfo holds specific information about the scheme the CRD runs under.
type schemeInfo struct {
	SchemeGroupVersion schema.GroupVersion
	SchemeBuilder      runtime.SchemeBuilder
	AddToScheme        func(s *runtime.Scheme) error
}

// crdScheme returns the pre-definied scheme information for the CRD.
func crdScheme() schemeInfo {
	output := schemeInfo{}
	output.SchemeGroupVersion = groupVersion()
	output.SchemeBuilder = runtime.NewSchemeBuilder(addTypesToSchema)
	output.AddToScheme = output.SchemeBuilder.AddToScheme

	return output
}

// add Types to Schema registers the Telemetry Policy CRD structs with the kubernetes API Group.
func addTypesToSchema(scheme *runtime.Scheme) error {
	SchemeGroupVersion := groupVersion()
	scheme.AddKnownTypes(SchemeGroupVersion,
		&telemetrypolicy.TASPolicy{},
		&telemetrypolicy.TASPolicyList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)

	return nil
}
