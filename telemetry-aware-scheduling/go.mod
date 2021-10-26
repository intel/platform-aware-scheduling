module github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling

go 1.16

require (
	github.com/intel/platform-aware-scheduling/extender v0.0.0-00010101000000-000000000000
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/klog/v2 v2.30.0
	k8s.io/metrics v0.22.2
)

replace (
	github.com/intel/platform-aware-scheduling/extender => ../extender
	github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling => ../telemetry-aware-scheduling
)
