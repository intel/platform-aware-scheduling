package metrics

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/api/resource"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"time"
)
//Mocks used for testing in the metrics and other packages
func DummyRestClientConfig() *restclient.Config {
	tmpFile, err := ioutil.TempFile("", "cmdtests_temp")
	if err != nil {
		panic(fmt.Sprintf("unable to create a fake client config: %v", err))
	}
	loadingRules := &clientcmd.ClientConfigLoadingRules{
		Precedence:     []string{tmpFile.Name()},
		MigrationRules: map[string]string{},
	}
	overrides := &clientcmd.ConfigOverrides{ClusterDefaults: clientcmdapi.Cluster{Server: "http://localhost:8080"}}
	fallbackReader := bytes.NewBuffer([]byte{})
	clientConfig := clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules, overrides, fallbackReader)
	restConfig, _ := clientConfig.ClientConfig()
	return restConfig
}

type DummyMetricsClient struct {
	store *map[string]NodeMetricsInfo
}

var InstanceOfMockMetricClientMap = map[string]NodeMetricsInfo{
	"dummyMetric1": TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{50, 30}),
	"dummyMetric2": TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{50, 30}),
	"dummyMetric3": TestNodeMetricCustomInfo([]string{"node A", "node B"}, []int64{50, 30}),
}

func NewDummyMetricsClient(cache map[string]NodeMetricsInfo) Client {
	return DummyMetricsClient{
		&cache,
	}
}

func (d DummyMetricsClient) GetNodeMetric(metricName string) (NodeMetricsInfo, error) {
	s := *d.store
	if v, ok := s[metricName]; ok {
		return v, nil
	}
	return nil, errors.New("metric not found")
}

func TestNodeMetricCustomInfo(nodeNames []string, numbers []int64) NodeMetricsInfo {
	n := NodeMetricsInfo{}
	for i, name := range nodeNames {
		n[name] = NodeMetric{Value: *resource.NewQuantity(numbers[i], resource.DecimalSI), Window: time.Second, Timestamp: time.Unix(100, 1)}
	}
	return n
}
