# Hardware trait matching with Prometheus and TAS

This tutorial will walk through the process of expressing hardware in terms of a capability by combining live telemetry and more static traits. This demo is designed for Cloudnative Network Functions and has complex requirements for both hardware and software with the recommended starting point for the demo being the the [Bare Metal Container Experience Kit](https://github.com/intel/container-experience-kits). There is a very high degree of configuration complexity in this demo as it's partial purpose was to demonstrate out-of-tree NUMA aware scheduling using Prometheus to calculate the topology characteristics of the nodes. As such it's recommended to fully read the tutorial to understand the requirements - including specific hardware requirments - before attempting to replicate it. 

**Note:** This demo was created to accompany the talk [Predictable Performance Through Prometheus and Topology Aware Scheduling](https://kccnceu20.sched.com/event/ZeqI/topology-aware-scheduling-using-prometheus-and-telemetry-aware-scheduler-swati-sehgal-intel-tom-golway-hewlett-packard-enterprise) at Kubecon EU 2020.

## Prerequisites

This demo uses a number of Kubernetes components related to Cloud native Network Function enablement. This demo was created using Intel's [Bare Metal Container Experience Kit](https://github.com/intel/container-experience-kits) (BMRA). This package includes everything needed for the demo except some of the data sources noted below.

### Kubernetes config

The demo requires at least Kubernetes version 1.18. The below policies will have to be set in the Kubelet configuration.

- CPU Manager Policy: Static
- Topology Manager Policy: Single Numa Node

### Kubernetes advanced networking

The demo uses SR-IOV networking to demonstrate scheduling of complex Cloudnative Network Functions. As a result the following are required:

#### Hardware
This demo assumes Intel X710 SR-IOV Enabled NICs with up to date firmware and drivers are in use.

#### Kubernetes SR-IOV Enablement
To enable SR-IOV Virtual Functions the following are needed:

* [Multus](https://github.com/intel/multus-cni): To enable secondary Network Interfaces in Kubernetes
* [SR-IOV CNI](https://github.com/intel/sriov-cni): To configure SR-IOV Virtual Functions
* [SR-IOV Device Plugin](https://github.com/intel/sriov-network-device-plugin): To manage the SR-IOV Virtual Functions as a Kubernetes resource.

This demo assumes the availability of 8 SR-IOV Virtual Functions with a vfio userspace driver installed at the outset.

### Telemetry Aware Scheduling and Prometheus

Telemetry Aware Scheduling and Prometheus should be up and running and working on the host. This will be the case by default when running the BMRA. To install TAS on a cluster, please follow the [quick start guide](https://github.com/intel/telemetry-aware-scheduling).

### Data sources
A number of data sources need to be deployed in order to fulfill our hardware trait.

* [collectd with platform power](/telemetry-aware-scheduling/docs/power) is needed to expose platform power metrics. A guide to setting up this exporter, with the associated Prometheus configs, is available [on this repo.](/telemetry-aware-scheduling/docs/power) 
* [SR-IOV Network Metrics Exporter](https://github.com/intel/sriov-network-metrics-exporter) is required for both up to date telemetry from Intel X710 NICs and NUMA topology information which is used to guarantee performance. This exporter is also used to gather basic information on CPU usage by workloads. 
* [Prometheus Node Exporter](https://github.com/prometheus/node_exporter) is used to gather information on memory availability for the workload. 

## Creating a hardware trait

The hardware trait our workload is looking for is ** 10GB Networking at 1 Million Packets per Second **. This is a complicated ask, with a bunch of features required in order to deliver on that promise. The trait is created by making a filter in Prometheus which starts off with all Numa alignments on the host and then excludes numa nodes from the filter (giving them a score of -1) if they don't have the available resources.

** Note: ** The above numbers are given for the purposes of demonstration only and should not be understood as representative of performance.

In order to get the above trait we need: 

1) Available CPUs with fast enough clock speeds:

```
((sum by(instance, numa_node) (sriov_cpu_info) - on(instance, numa_node) sum by(instance, numa_node) (sriov_kubepodcpu)) or sum by(instance, numa_node) (sriov_cpu_info) >= 9)

and (count by(instance, numa_node) ((sriov_cpu_info * on(cpu, instance) group_right(numa_node) node_cpu_frequency_max_hertz) > 3.5e+09))
```

The above query uses cpu information available from the SR-IOV Metrics Exporter and pairs it with information about max cpu frequency from the Prometheus node exporter. It outputs a list of Numa Zones / Compute Nodes that have at least 9 cpus available that can operate at a max frequency of 3.5 GHz

2) Available memory HugePages:

```
and on(instance) (node_memory_HugePages_Free> 10)
```
This query excludes nodes that have fewer than 10 free memory HugePages.

3) Available power headroom on the node:

```
and on(instance) ((collectd_package_0_TDP_power_power + collectd_package_1_TDP_power_power) - (collectd_package_0_power_power + collectd_package_1_power_power) > 150)
```
This query excludes nodes that have less than the required 150 W of platform power headroom available.

4) Available SR-IOV Virtual Functions (By NUMA node) with uncontested free bandwidth

```
and on(instance, numa_node) (count by(instance, numa_node) (sriov_vf_rx_bytes unless on(pciAddr) sriov_vf_rx_bytes - on(pciAddr) sriov_kubepoddevice) >= 8)
and on(instance,numa_node) (count by(instance, pf, numa_node) (sriov_vf_max_rate - on(instance, numa_node, pf) sum by(instance, numa_node, pf) (rate(sriov_vf_rx_bytes[1m]) + rate(sriov_vf_tx_bytes[1m])) >= 1e+10))
```
Here we look for SR-IOV Virtual functions per numa zone and exclude zones where there are fewer than 8 available. Additionally we use up to date telemetry to ensure that there is no contention on the Physical Function the SR-IOV Virtual Function belongs to.

All of the above is wrapped in a `clamp_max( clamp_min(` which means the end result will be a categorical number of 1 or -1 which can easily be understood by TAS. 

The whole resulting query for our single hardware trait is:

```
##Set min/max values
clamp_max( clamp_min(

##Check free cpus with Numa Alignment
((sum by(instance, numa_node) (sriov_cpu_info) - on(instance, numa_node) sum by(instance, numa_node) (sriov_kubepodcpu)) or sum by(instance, numa_node) (sriov_cpu_info) >= 9)

##Check for cpu frequency cap
and (count by(instance, numa_node) ((sriov_cpu_info * on(cpu, instance) group_right(numa_node) node_cpu_frequency_max_hertz) > 3.5e+09))

##Check for free HugePages
and on(instance) (node_memory_HugePages_Free> 10)

##Check for power headroom
and on(instance) ((collectd_package_0_TDP_power_power + collectd_package_1_TDP_power_power) - (collectd_package_0_power_power + collectd_package_1_power_power) > 150)

##Check for number of devices
and on(instance, numa_node) (count by(instance, numa_node) (sriov_vf_rx_bytes unless on(pciAddr) sriov_vf_rx_bytes - on(pciAddr) sriov_kubepoddevice) >= 8)

##Check enough bandwidth is available
and on(instance,numa_node) (count by(instance, pf, numa_node) (sriov_vf_max_rate - on(instance, numa_node, pf) sum by(instance, numa_node, pf) (rate(sriov_vf_rx_bytes[1m]) + rate(sriov_vf_tx_bytes[1m])) >= 1e+10))

##Add negative scores for missing nodes
or (sum by(instance, numa_node) (sriov_cpu_info) - 9999), -1), 1)
```

This query will output a -1 for each node with at least one NUMA zone where placement is possible and a -1 for nodes with no NUMA zones where placement is possible.

## Making a trait available in the Kubernetes API
Telemetry Aware Scheduling uses the Kuberenetes Custom Metrics API to access data. To make the above trait available through the Prometheus Custom Metrics Adapter we first express it as a single metric in Prometheus using a Prometheus rule, and then configure the Prometheus Adapter to scrape the metric.

In your Prometheus Rules file add:
```
    groups:
    - name: hardware traits 
      rules:
      - record: traits_10GB1Mpps
        expr: clamp_max( clamp_min(((sum by(instance, numa_node) (sriov_cpu_info) - on(instance, numa_node) sum by(instance,numa_node) (sriov_kubepodcpu)) or sum by(instance, numa_node) (sriov_cpu_info) >= 9)and (count by(instance, numa_node) ((sriov_cpu_info * on(cpu, instance) group_right(numa_node) node_cpu_frequency_max_hertz) > 3.5e+09))and on(instance) (node_memory_HugePages_Free> 10)and on(instance) ((collectd_package_0_TDP_power_power + collectd_package_1_TDP_power_power) - (collectd_package_0_power_power + collectd_package_1_power_power) > 150)and on(instance, numa_node) (count by(instance, numa_node) (sriov_vf_rx_bytes unless on(pciAddr) sriov_vf_rx_bytes - on(pciAddr) sriov_kubepoddevice) >= 8)and on(instance,numa_node) (count by(instance, pf, numa_node) (sriov_vf_max_rate - on(instance, numa_node, pf) sum by(instance, numa_node, pf) (rate(sriov_vf_rx_bytes[1m]) + rate(sriov_vf_tx_bytes[1m])) >= 1e+10))or (sum by(instance, numa_node) (sriov_cpu_info) - 9999), -1), 1)

``` 
This huge query is the same as described in the above section. Now that it is added as a rule it will be calculated on each Prometheus cycle and will be queryable by name in the Prometheus Database. 

Once the trait is queryable in Prometheus we need to give instruction to the custom metrics adapter on how to query it. Add a new query config to the custom metrics adapter:

```
    - metricsQuery: <<.Series>>
      name:
        matches: ^trait_(.*)
      resources:
        overrides:
          instance:
            resource: trait
      seriesQuery: '{__name__=~"^trait_.*"}'

```
This tells the adapter to search for queries starting with "trait_" and match them to Kubernetes Nodes in the api.

We should now be able to query the API with:

``kubectl get --raw /apis/custom.metrics.k8s.io/v1beta1/nodes/*/10GB1Mpps``

The output should be a json object with a score for each node in the cluster.

## Making a trait part of a Telemetry Scheduling Policy

Telemetry Aware Scheduling works on the basis of policies which can be shared between workloads. Now that we can query the trait metric we can create a TAS policy like:
```
apiVersion: telemetry.intel.com/v1alpha1
kind: TASPolicy
metadata:
  name: hardware-traits
  namespace: default
spec:
  strategies:
    dontschedule:
      rules:
      - metricname: 10GB1Mpps
        operator: Equals
        target: -1
```
This policy can be created by running:

` kubectl apply -f scheduling-policy.yaml` 

This tells TAS to not schedule workloads to nodes that score a -1 on our trait. If our workload had more than one trait those could also be added here to produce a multi-trait scheduling ask for workloads with this specific policy.

## Scheduling a workload based on a Hardware Trait
Now that the preparatory work is done we can schedule our workload with:
`
kubectl apply -f cloudnative-network-function.yaml
`
With TAS and Prometheus working to ensure topology alignment, among other factors, our workload will not be scheduled until hardware meeting our defined trait becomes available.

## Additional material

The other scripts used in the Kubecon demo, including the deployment file for our power hungry workload and the script to run a packet processing workload are also included in this repo.
