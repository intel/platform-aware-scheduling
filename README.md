# Platform Aware Scheduling
Platform Aware Scheduling (PAS) contains a group of related projects designed to expose platform specific attributes to the Kubernetes scheduler using a modular policy driven approach. The project contains a core library and information for building custom scheduler extensions as well as specific  implementations that can be used in a working cluster or leveraged as a reference for creating new Kubernetes scheduler extensions.

Telemetry Aware Scheduling is the initial reference implementation of Platform Aware Scheduling. It can expose any platform-level metric to the Kubernetes Scheduler for policy driven filtering and prioritization of workloads. You can read more about TAS [here](/telemetry-aware-scheduling).

GPU Aware Scheduling is the implementation of the GPU resource aware Kubernetes scheduler extension.


* [Kubernetes Scheduler Extenders](#kubernetes-scheduler-extenders)
* [Extenders](#plugins)
    * [Telemetry Aware Scheduling](/telemetry-aware-scheduling)
    * [GPU Aware Scheduling](/gpu-aware-scheduling)
* [Communication and contribution](#communication-and-contribution)

## Kubernetes Scheduler Extenders

Platform Aware Scheduling leverages the power of Kubernetes Scheduling Extenders. These extenders allow the core Kubernetes scheduler to make HTTP calls to an external service which can then modify scheduling decisions. This can be used to provide workload specific scheduling direction based on attributes not normally exposed to the Kubernetes scheduler.

The [extender](/extender) package at the top-level of this repo can be used to quickly create a working scheduler extender.

### Enabling a scheduler extender

Scheduler extenders are enabled by providing a scheduling configuration file to the default Kubernetes scheduler. An example of a configuration file:

````
apiVersion: kubescheduler.config.k8s.io/v1beta2
kind: KubeSchedulerConfiguration
clientConnection:
  kubeconfig: /etc/kubernetes/scheduler.conf
extenders:
  - urlPrefix: "https://tas-service.default.svc.cluster.local:9001"
    prioritizeVerb: "scheduler/prioritize"
    filterVerb: "scheduler/filter"
    weight: 1
    enableHTTPS: true
    managedResources:
      - name: "telemetry/scheduling"
        ignoredByScheduler: true
    ignorable: true
    tlsConfig:
      insecure: false
      certFile: "/host/certs/client.crt"
      keyFile: "/host/certs/client.key"

````

There are a number of options available to us under the "extenders" configuration object. Some of these fields - such as setting the urlPrefix, filterVerb and prioritizeVerb are necessary to point the Kubernetes scheduler to our scheduling service, while other sections deal the TLS configuration of mutual TLS. The remaining fields tune the behavior of the scheduler: managedResource is used to specify which pods should be scheduled using this service, in this case pods which request the dummy resource telemetry/scheduling, ignorable tells the scheduler what to do if it can't reach our extender and weight sets the relative influence our extender has on prioritization decisions.

With a configuration like the above as part of the Kubernetes scheduler configuration the identified webhook becomes part of the scheduling process.

To read more about scheduler extenders see the [official docs](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/scheduling/scheduler_extender.md).
 
## Adding a new extender to Platform Aware Scheduling
Platform Aware Scheduling is a single repo designed to host multiple hardware enabling Kubernetes Scheduler Extenders. A new scheduler can be added with an issue and pull request.

Each project under the top-level repo has its own go module, dependency model and lifecycle.There is no single top level go.mod for the project. Some development tools and testing workflows may need to be done in the context of the go module they're targeting i.e. by changing into one of the directories that contains a go module.

## Communication and contribution

Report a bug by [filing a new issue](https://github.com/intel/telemetry-aware-scheduling/issues).

Contribute by [opening a pull request](https://github.com/intel/telemetry-aware-scheduling/pulls).

Learn [about pull requests](https://help.github.com/articles/using-pull-requests/).

**Reporting a Potential Security Vulnerability:** If you have discovered potential security vulnerability in TAS, please send an e-mail to secure@intel.com. For issues related to Intel Products, please visit [Intel Security Center](https://security-center.intel.com).

It is important to include the following details:

- The projects and versions affected
- Detailed description of the vulnerability
- Information on known exploits

Vulnerability information is extremely sensitive. Please encrypt all security vulnerability reports using our [PGP key](https://www.intel.com/content/www/us/en/security-center/pgp-public-key.html).

A member of the Intel Product Security Team will review your e-mail and contact you to collaborate on resolving the issue. For more information on how Intel works to resolve security issues, see: [vulnerability handling guidelines](https://www.intel.com/content/www/us/en/security-center/vulnerability-handling-guidelines.html).

