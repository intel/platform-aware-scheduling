//Package client telemetrypolicy/api/client provides an interface to interact with Policy CRD through a custom Client.
package client

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

//Client holds the information needed to query telemetry policies from the kubernetes API.
type Client struct {
	rest           *rest.RESTClient
	namespace      string
	plural         string
	parameterCodec runtime.ParameterCodec
}
