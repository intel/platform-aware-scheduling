package cache

import (
	"encoding/json"
	"fmt"
	"github.com/intel/telemetry-aware-scheduling/pkg/metrics"
	telemetrypolicy "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
	"net/http"
)

//RemoteClient can send http requests to a single endpoint
type RemoteClient struct {
	endpoint string
	http.Client
}

//RegisterEndpoint adds an endpoint for the metrics getter to read from
func (r *RemoteClient) RegisterEndpoint(endpoint string) {
	r.endpoint = endpoint
}

//ReadMetric sends a read request for the passed metric to the remote cache
func (r *RemoteClient) ReadMetric(metricName string) (metrics.NodeMetricsInfo, error) {
	path := r.endpoint + "metrics/" + metricName
	resp, err := r.Get(path)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, err
	}
	result := metrics.NodeMetricsInfo{}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("error decoding metric request: %v", err)
	}
	return result, nil
}
//ReadPolicy sends a read request for the passed namespace/name policy to the remote cache
func (r *RemoteClient) ReadPolicy(namespace string, policyName string) (telemetrypolicy.TASPolicy, error) {
	path := r.endpoint + "policies/" + namespace + "/" + policyName
	resp, err := r.Get(path)
	result := telemetrypolicy.TASPolicy{}
	if err != nil || resp.StatusCode != http.StatusOK {
		return result, err
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("error decoding policy request: %v", err)
	}
	return result, nil
}
