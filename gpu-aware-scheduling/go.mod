module github.com/intel/platform-aware-scheduling/gpu-aware-scheduling

go 1.16

require (
	github.com/intel/platform-aware-scheduling/extender v0.0.0-00010101000000-000000000000
	github.com/smartystreets/goconvey v1.7.0
	github.com/stretchr/testify v1.7.0
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/klog/v2 v2.30.0
)

replace (
	github.com/intel/platform-aware-scheduling/extender => ../extender
	github.com/intel/platform-aware-scheduling/gpu-aware-scheduling => ../gpu-aware-scheduling
)
