//Provides a controller that can be used to watch policies in the Kuebrnetes API.
//It registers strategies from those policies to an enforcer.
package controller

import (
	"context"
	"github.com/intel/telemetry-aware-scheduling/pkg/cache"
	strategy "github.com/intel/telemetry-aware-scheduling/pkg/strategies/core"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/rest"
)

var mockServer = httptest.Server{
	URL: "localhost:9090",
}

func TestTelemetryPolicyController_Run(t *testing.T) {
	type fields struct {
		TelemetryPolicyClient rest.Interface
		Cache                 cache.ReaderWriter
		Enforcer              strategy.Enforcer
	}
	type args struct {
		context context.Context
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		//{"basic test",
		//	fields{fakeRESTClient(), metrics.NewAutoUpdatingCache(), &strategy.MetricEnforcer{}},
		//	args{context.Background()},
		//},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &TelemetryPolicyController{
				tt.fields.TelemetryPolicyClient,
				tt.fields.Cache,
				tt.fields.Enforcer,
			}
			controller.Run(tt.args.context)
		})
		mockServer.Close()
	}
}