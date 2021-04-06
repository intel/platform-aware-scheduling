//Package v1alpha1 describes the structure of the Telemetry Policy CRD.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//Defines key values for policy CRD
const (
	Plural  = "taspolicies"
	Group   = "telemetry.intel.com"
	Version = "v1alpha1"
)

// TASPolicy is the Schema for the taspolicies API
type TASPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TASPolicySpec   `json:"spec"`
	Status TASPolicyStatus `json:"status,omitempty"`
}

//TASPolicyStrategy contains a set of TASPolicyRule which define the strategy.
type TASPolicyStrategy struct {
	PolicyName string          `json:"policyName"`
	Rules      []TASPolicyRule `json:"rules"`
}

//TASPolicyRule contains specific, implementable logic of a metric name, an operator and a target.
type TASPolicyRule struct {
	Metricname string `json:"metricname"`
	Operator   string `json:"operator"`
	Target     int64  `json:"target"`
}

//TASPolicySpec is a map of strategies indexed by their strategy type name i.e. scheduleonmetric, dontschedule.
type TASPolicySpec struct {
	Strategies map[string]TASPolicyStrategy `json:"strategies"`
}

// TASPolicyStatus defines the observed state of TASpolicy. Currently no status object is implemented.
//TODO: Implement policy status object
type TASPolicyStatus struct {
}

//TASPolicyList contains a list of TASpolicy
type TASPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TASPolicy `json:"items"`
}
