//Package deschedule provides the deschedule strategy. Violation conditions and enforcement behavior are defined here.
//When a node is violating the deschedule strategy, the enforcer labels it as violating.
//This label can then be used externally to act on the strategy violation.
package deschedule

import (
	"fmt"
	"log"

	"github.com/intel/telemetry-aware-scheduling/pkg/cache"
	"github.com/intel/telemetry-aware-scheduling/pkg/strategies/core"
	telempol "github.com/intel/telemetry-aware-scheduling/pkg/telemetrypolicy/api/v1alpha1"
)

//StrategyType is set to de-schedule.
const (
	StrategyType = "deschedule"
)

//Strategy type for de-scheduling from a single policy.
type Strategy telempol.TASPolicyStrategy

//StrategyType returns the name of the strategy type. This is used to place it in the registry.
func (d *Strategy) StrategyType() string {
	return StrategyType
}

//Violated checks to see if the strategy is violated by searching for nodes that have metrics that don't accord with the target in descheduling strategy.
//Returns a map of nodeNames as key with an empty value associated with each.
func (d *Strategy) Violated(cache cache.Reader) map[string]interface{} {
	violatingNodes := map[string]interface{}{}
	for _, rule := range d.Rules {
		nodeMetrics, err := cache.ReadMetric(rule.Metricname)
		if err != nil {
			log.Print(err)
			continue
		}
		for nodeName, nodeMetric := range nodeMetrics {
			log.Print(nodeName+" "+rule.Metricname, " = ", nodeMetric.Value.AsDec())
			if core.EvaluateRule(nodeMetric.Value, rule) {
				log.Print(nodeName + " violating " + d.PolicyName + ": " + ruleToString(rule))
				violatingNodes[nodeName] = nil
			}
		}
	}
	return violatingNodes
}

//ruleToString returns the rule passed to it as a single string
func ruleToString(rule telempol.TASPolicyRule) string {
	return fmt.Sprintf("%v %v %v", rule.Metricname, rule.Operator, rule.Target)
}

//Equals checks if a strategy is the same as the passed strategy.
//It can be used to prevent duplication of strategies in the API and is also used to find strategies for deletion.
//TODO: Remedial action if equal, i.e. point to other strategies. Make method order ambivalent
func (d *Strategy) Equals(other core.Interface) bool {
	otherDeschedulerStrategy, ok := other.(*Strategy)
	sameName := other.GetPolicyName() == d.GetPolicyName()
	if ok && sameName && len(d.Rules) > 0 && len(d.Rules) == len(otherDeschedulerStrategy.Rules) {
		for i, rule := range d.Rules {
			if rule.Metricname != otherDeschedulerStrategy.Rules[i].Metricname {
				return false
			}
			if rule.Target != otherDeschedulerStrategy.Rules[i].Target {
				return false
			}
			if rule.Operator != otherDeschedulerStrategy.Rules[i].Operator {
				return false
			}
		}
		return true
	}
	return false
}

//GetPolicyName returns the name of the policy that originated strategy
func (d *Strategy) GetPolicyName() string {
	return d.PolicyName
}

//SetPolicyName adds a policy name to be associated with this strategy.
func (d *Strategy) SetPolicyName(name string) {
	d.PolicyName = name
}
