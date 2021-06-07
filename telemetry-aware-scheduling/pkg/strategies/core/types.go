//Contains the behaviour shared between all Violable and Enforceable strategies from a Telemetry Policy.

package core

import (
	"time"

	"github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling/pkg/cache"
)

//Interface describes expected behavior of a specific strategy.
type Interface interface {
	Violated(cache cache.Reader) map[string]interface{}
	Enforce(enforcer *MetricEnforcer, cache cache.Reader) (int, error)
	StrategyType() string
	Equals(Interface) bool
	GetPolicyName() string
	SetPolicyName(string)
}

//Enforcer registers strategies by type, adds specific strategies to a registry, and Enforces those strategies.
type Enforcer interface {
	RegisterStrategyType(strategy Interface)
	UnregisterStrategyType(strategy Interface)
	IsRegistered(string) bool
	AddStrategy(Interface, string)
	RemoveStrategy(Interface, string)
	EnforceRegisteredStrategies(cache.Reader, time.Ticker)
}
