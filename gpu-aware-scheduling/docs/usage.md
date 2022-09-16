# Usage with NFD and GPU-plugin
This document explains how to get GAS working together with [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery) (NFD) and the [GPU-plugin](https://github.com/intel/intel-device-plugins-for-kubernetes/blob/main/cmd/gpu_plugin/README.md).

To begin with, it will help a lot if you have been successful already using the Intel GPU-plugin with
some deployments. That means your HW and cluster is most likely fine with GAS also.

## Enabling fractional resource support in GPU-plugin and NFD
Resource management is required to be enabled in GPU-plugin currently to run GAS. With resource
management enabled, GPU-plugin can read the necessary annotations of the PODs. Without reading
those annotations, GPU allocations will not work correctly. NFD needs to be configured to create
the node extended resources which are then used by GAS.

The easiest way to setup both the Intel GPU device plugin and NFD is to follow the
[installation instructions of the Intel GPU-plugin](https://github.com/intel/intel-device-plugins-for-kubernetes/blob/main/cmd/gpu_plugin/README.md#install-to-nodes-with-intel-gpus-with-fractional-resources).

## Cluster nodes

You need some i915 GPUs in the nodes. Integrated GPUs work fine for testing GAS, most recent NUCs are good enough.

## PODs

Your PODs need to ask for GPU-resources, for instance:
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

A complete example pod yaml is located in [docs/example](./example)

## Node Label support

GAS supports certain node labels as a means to allow telemetry based GPU selection decisions and
descheduling of PODs using a certain GPU. You can create node labels with the
[Telemetry Aware Scheduling](../../telemetry-aware-scheduling/README.md) labeling strategy,
which puts them in its own namespace. In practice the supported labels need to be in the
`telemetry.aware.scheduling.POLICYNAME/`[^1] namespace.

The node label `gas-deschedule-pods-GPUNAME`[^2] will result in GAS labeling the PODs which
use the named GPU with the `gpu.aware.scheduling/deschedule-pod=gpu` label. So TAS labels the node,
and based on the node label GAS finds and labels the PODs. You may then use a kubernetes descheduler
to pick the pods for descheduling via their labels.

The node label `gas-disable-GPUNAME`[^2] will result in GAS stopping the use of the named GPU for new
allocations.

The node label `gas-prefer-gpu=GPUNAME`[^2] will result in GAS trying to use the named
GPU for new allocations before other GPUs of the same node.

Note that the value of the labels starting with `gas-deschedule-pods-GPUNAME`[^2] and
`gas-disable-GPUNAME`[^2] doesn't matter. You may use e.g. "true" as the value. The only exception to
the rule is `PCI_GROUP` which has a special meaning, explained separately. Example:
`gas-disable-card0=PCI_GROUP`.

[^1]: POLICYNAME is defined by the name of the TASPolicy. It can vary.
[^2]: GPUNAME can be e.g. card0, card1, card2… which corresponds to the gpu names under `/dev/dri`.

### PCI Groups

If GAS finds a node label `gas-disable-GPUNAME=PCI_GROUP`[^2] the disabling will impact a
group of GPUs which is defined in the node label `gpu.intel.com/pci-groups`. The syntax of the
pci group node label is easiest to explain with an example: `gpu.intel.com/pci-groups=0.1_2.3.4`
would indicate there are two pci-groups in the node separated with an underscore, in which card0
and card1 form the first group, and card2, card3 and card4 form the second group. If GAS would
find the node label `gas-disable-card3=PCI_GROUP` in a node with the previous example PCI-group
label, GAS would stop using card2, card3 and card4 for new allocations, as card3 belongs in that
group.

`gas-deschedule-pods-GPUNAME`[^2] supports the PCI_GROUP value similarly, the whole group in which
the named gpu belongs, will end up descheduled.

The PCI group feature allows for e.g. having a telemetry action to operate on all GPUs which
share the same physical card.

## Allowlist and Denylist

You can use POD-annotations in your POD-templates to list the GPU names which you allow, or deny
for your deployment. The values for the annotations are comma separated value lists of the form
"card0,card1,card2", and the names of the annotations are:

- `gas-allow`
- `gas-deny`

Note that the feature is disabled by default. You need to enable allowlist and/or denylist via command line flags.

## Enforcing same gpu to multiple containers within Pod

By default when GAS checks if available Node resources are enough for Pod's resources requests,
the containers of the Pod are processed sequentially and independently. In multi-gpu nodes in
certain cases this may result (but not guaranteed) in containers of the same Pod having different
GPUs allocated to them.

In case two or more containers of the same Pod require to use the same GPU, GAS supports
`gas-same-gpu` Pod annotation (value is a list of container names) that tells GAS which containers
should only be given the same GPU. In case if neither of the GPUs on the node have enough available
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

## Summary in a chronological order

- GPU-plugin initcontainer installs an NFD hook which prints labels for you, based on the Intel GPUs it finds
- NFD creates extended resources for you, and labels the nodes, based on the labels the hook prints
- Your POD specs must include resource limits for GPU
- GAS filters out nodes from deployments when necessary, and it annotates PODs
- GPU-plugin reads annotations from the PODs and selects the GPU based on those

## Some troubleshooting tips

Check the logs (kubectl logs podname -n namespace) from all of these when in trouble. Also check k8s scheduler logs. Finally check the POD description and logs for your deployments.

- Check that GPU-plugin initcontainer runs happily, and installs the hook at /etc/kubernetes/node-feature-discovery/source.d/
- Check that NFD picks up the labels without complaints, no errors in NFD workers or the master
- Check that your GPU-enabled nodes have NFD-created GPU extended resources (kubectl describe node nodename) and GPU-labels
- Check the log of GAS POD. If the log does not show anything ending up happening during deploying of i915 resource consuming PODs, your scheduler extender setup may be incorrect. Verify that you have successfully run all the deployment steps and the related cluster setup script.
- Check the GPU plugin logs
