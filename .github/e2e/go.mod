module github.com/intel/platform-aware-scheduling/e2e

go 1.16

require (
	github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling v0.1.0
	github.com/pkg/errors v0.9.1
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/klog/v2 v2.30.0
)

replace (
	github.com/intel/platform-aware-scheduling/extender => ../../extender
	github.com/intel/platform-aware-scheduling/telemetry-aware-scheduling => ../../telemetry-aware-scheduling
)
