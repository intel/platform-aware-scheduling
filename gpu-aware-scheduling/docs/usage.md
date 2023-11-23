# Usage with NFD and GPU-plugin
This document explains how to get GAS working together with [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery) (NFD) and the [GPU-plugin](https://github.com/intel/intel-device-plugins-for-kubernetes/blob/main/cmd/gpu_plugin/README.md).

It will help a lot if you have already been successfully using the Intel GPU-plugin with
some deployments. That means your HW and cluster is most likely fine also with GAS.

## Enabling fractional resource support in GPU-plugin and NFD
Resource management must be enabled in GPU-plugin to run GAS. With resource
management enabled, GPU-plugin will use annotations GAS adds to Pods,
otherwise GPU allocations will not work correctly. NFD needs also to be
configured for node extended resources used by GAS to be created.

The easiest way to setup both the Intel GPU device plugin and NFD is to follow the
[installation instructions of the Intel GPU-plugin](https://github.com/intel/intel-device-plugins-for-kubernetes/blob/main/cmd/gpu_plugin/README.md#install-to-nodes-with-intel-gpus-with-fractional-resources).

## Cluster nodes

You need some Intel GPUs in the nodes. Even integrated GPUs work fine for testing GAS.

## Pods

Your Pods need to ask for GPU-resources, for instance:
```YAML
        resources:
          limits:
            gpu.intel.com/i915: 1
            gpu.intel.com/millicores: 10
            gpu.intel.com/memory.max: 10M
```

Or, for tiles:
```YAML
        resources:
          limits:
            gpu.intel.com/i915: 1
            gpu.intel.com/tiles: 2
```

A complete example Pod yaml is located in [docs/example](./example)

## Node Label support

GAS supports certain node labels as a means to allow telemetry based GPU selection decisions and
descheduling of Pods using a certain GPU. You can create node labels with the
[Telemetry Aware Scheduling](../../telemetry-aware-scheduling/README.md) labeling strategy,
which puts them in its own namespace. In practice the supported labels need to be in the
`telemetry.aware.scheduling.POLICYNAME/`[^1] namespace.

The node label `gas-deschedule-pods-GPUNAME`[^2] will result in GAS labeling the Pods which
use the named GPU with the `gpu.aware.scheduling/deschedule-pod=gpu` label. So TAS labels the node,
and based on the node label GAS finds and labels the Pods. You may then use a kubernetes descheduler
to pick the Pods for descheduling via their labels.

The node label `gas-disable-GPUNAME`[^2] will result in GAS stopping the use of the named GPU for new
allocations.

The node label `gas-prefer-gpu=GPUNAME`[^2] will result in GAS trying to use the named
GPU for new allocations before other GPUs of the same node.

Note that the value of the labels starting with `gas-deschedule-pods-GPUNAME`[^2] and
`gas-disable-GPUNAME`[^2] doesn't matter. You may use e.g. "true" as the value. The only exception to
the rule is `PCI_GROUP` which has a special meaning, explained separately. Example:
`gas-disable-card0=PCI_GROUP`.

[^1]: POLICYNAME is defined by the name of the TASPolicy. It can vary.
[^2]: GPUNAME can be e.g. card0, card1, card2… which corresponds to the GPU names under `/dev/dri`.

### PCI Groups

If GAS finds a node label `gas-disable-GPUNAME=PCI_GROUP`[^2] the disabling will impact a
group of GPUs which is defined in the node label `gpu.intel.com/pci-groups`.
For example the PCI-group node label `gpu.intel.com/pci-groups=0.1_2.3.4`
would indicate there are two PCI-groups in the node separated with an underscore, in which card0
and card1 form the first group, and card2, card3 and card4 form the second group. If GAS would
find the node label `gas-disable-card3=PCI_GROUP` in a node with the previous example PCI-group
label, GAS would stop using card2, card3 and card4 for new allocations, as card3 belongs in that
group.

`gas-deschedule-pods-GPUNAME`[^2] supports the PCI_GROUP value similarly, the whole group in which
the named GPU belongs, will end up descheduled.

The PCI group feature allows for e.g. having a telemetry action to operate on all GPUs which
share the same physical card.

## Allowlist and Denylist

You can use Pod-annotations in your Pod-templates to list the GPU names which you allow, or deny
for your deployment. The values for the annotations are comma separated value lists of the form
"card0,card1,card2", and the names of the annotations are:

- `gas-allow`
- `gas-deny`

Note that the feature is disabled by default. You need to enable allowlist and/or denylist via command line flags.

## Enforcing same GPU to multiple containers within Pod

By default when GAS checks if available Node resources are enough for Pod's resources requests,
the containers of the Pod are processed sequentially and independently. In multi-GPU nodes in
certain cases this may result (but not guaranteed) in containers of the same Pod having different
GPUs allocated to them.

In case two or more containers of the same Pod require to use the same GPU, GAS supports
`gas-same-gpu` Pod annotation (value is a list of container names) that tells GAS which containers
should only be given the same GPU. In case none of the GPUs on the node have enough available
resources for all containers listed in such annotation, the current node will not be used for
scheduling.

<details>
<summary>Example Pod annotation</summary>

```YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-app
  labels:
    app: demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: demo 
  template:
    metadata:
      labels:
        app: demo
      annotations:
        gas-same-gpu: busybox1,busybox2
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            gpu.intel.com/i915: 1
            gpu.intel.com/millicores: 400
      - name: busybox2
        image: busybox:latest
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            gpu.intel.com/i915: 1
            gpu.intel.com/millicores: 100
        command: ["/bin/sh", "-c", "sleep 3600"]
      - name: busybox1
        image: busybox:latest
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            gpu.intel.com/i915: 1
            gpu.intel.com/millicores: 100
        command: ["/bin/sh", "-c", "sleep 3600"]
```

</details>

### Restrictions

- Containers listed in `gas-same-gpu` annotation have to request exactly one `gpu.intel.com/i915` resource
- Containers listed in `gas-same-gpu` annotation cannot request `gpu.intel.com/i915_monitoring` resource
- Containers listed in `gas-same-gpu` annotation cannot request `gpu.intel.com/tiles` resource
- Pods cannot use (single-GPU) `gas-same-gpu` annotation with (multi-GPU) `gas-allocate-xelink` annotation

## Multi-GPU allocation with Xe Link connections

In case your cluster nodes have fully connected Xe Links between all GPUs, you need not worry about
allocating a pair of GPUs and tiles which have an Xe Link between them, because all do.

For clusters which have nodes that have only some of the GPUs connected with Xe Links, GAS offers the
possibility to allocate from only those GPUs which happen to have an Xe Link between them. Such cluster nodes
need to have the label `gpu.intel.com/xe-links`, which describes the xe-links of the node.

Label example:
`gpu.intel.com/xe-links=0.0-1.0_2.0-3.0`, where `_` separates link descriptions, `-` separates linked GPUs and `.`
separates a Level-Zero deviceId from a subdeviceId number. Thus that examples reads: GPU device with id 0 subdevice
id 0 is connected with an Xe Link to GPU device with id 1 and subdevice id 0. GPU Device with id 2 subdevice id 0
is connected to GPU device with id 3 subdevice 0.

To instruct GAS to only allocate GPUs that have an Xe Link, Pod specification needs the `gas-allocate-xelink`
annotation. The value-part of the annotation can be anything (recommendation: `true`). When the annotation is
present, it impacts all containers of the Pod. Then GAS will only allocate GPUs which have free tiles that
have Xe Links between them listed in the `gpu.intel.com/xe-links` node-label.

<details>
<summary>Example Pod annotation</summary>

The Example Pod will allocate one tile (2/2) from each of the two requested GPUs.
In total 2 tiles will be consumed and devices from 2 GPUs will appear in the container.

```YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-app
  labels:
    app: demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: demo
  template:
    metadata:
      labels:
        app: demo
      annotations:
        gas-allocate-xelink: 'true'
    spec:
      containers:
      - name: busybox
        image: busybox:latest
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            gpu.intel.com/i915: 2
            gpu.intel.com/tiles: 2
        command: ["/bin/sh", "-c", "sleep 3600"]
```
</details>

### Restrictions

- In Pods with `gas-allocate-xelink` annotation, every GPU-using container within the same Pod:
  - Must request an even (not odd) amount of `gpu.intel.com/i915` resources
  - Must request as many `gpu.intel.com/tiles`[^3] as the `gpu.intel.com/i915` resources
  - Cannot request `gpu.intel.com/i915_monitoring` resource
- Pods cannot use (single-GPU) `gas-same-gpu` annotation with (multi-GPU) `gas-allocate-xelink` annotation

[^3]: Requires Pod spec to specify Level-Zero hierarchy mode expected by the workload (as that affects format of affinity mask set by the `tiles` resource), see [GPU-Plugin documentation](https://github.com/intel/intel-device-plugins-for-kubernetes/blob/main/cmd/gpu_plugin/README.md)

## Single-Numa GPU requests

When allocating multiple GPUs, Pod can request them to be from the same Numa node by adding annotation `gas-allocate-single-numa` with value `true`
to the Pod. The Kubernetes Topology Manager cannot be used with Pods that get GPUs assigned by GAS, but you can use
[CRI-RM](https://github.com/intel/cri-resource-manager) instead of the Topology Manager to get similar performance gains with CPUs selected from the
same Numa node.

## Summary in a chronological order

- GPU-plugin itself, or its initcontainer, and a GPU monitor sidecar, provide label information to NFD based on the found Intel GPU properties
- NFD creates extended resources and labels the nodes, based on the given information
- Your Pod specs must include resource limits for GPU
- GAS filters out nodes from deployments when necessary, and annotates Pods
- GPU-plugin reads annotations from the Pods and selects the GPU(s) based on those

## Some troubleshooting tips

Check the logs (kubectl logs podname -n namespace) from all of these when in trouble. Also check k8s scheduler logs. Finally check the Pod description and logs for your deployments.

- Check that GPU-plugin either:
  - Runs initcontainer which installs hook binary to `/etc/kubernetes/node-feature-discovery/source.d/`
  - Or GPU-plugin itself saves NFD feature file to `/etc/kubernetes/node-feature-discovery/features.d/`
- And for Xe Link connnectivity, check XPU Manager having a sidecar saving NFD feature file to same place
- Check that NFD picks up the labels without complaints, no errors in NFD workers or the master
- Check that your GPU-enabled nodes have NFD-created GPU extended resources (kubectl describe node nodename) and GPU-labels
- Check the log of GAS Pod. If the log does not show anything ending up happening during deploying of i915 resource consuming Pods, your scheduler extender setup may be incorrect. Verify that you have successfully run all the deployment steps and the related cluster setup script.
- Check the GPU plugin logs
