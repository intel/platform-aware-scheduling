package controller

import (
	"github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
	strategy "github.com/intel/telemetry-aware-scheduling/telemetry-aware-scheduling/pkg/strategies/core"
	"k8s.io/client-go/rest"
)

//TelemetryPolicyController instruments the necessary functions for to Register policies to a metrics cache and a Interface registry.
//Controller embeds a rest interface to Kubernetes which allows it to be passed as a client. It also embeds a cache editor which allows it to write to and delete from a shared cache.
type TelemetryPolicyController struct {
	rest.Interface
	cache.Writer
	Enforcer strategy.Enforcer
}
