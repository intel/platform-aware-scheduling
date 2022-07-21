# Usage with NFD and GPU-plugin
This document explains how to get GAS working together with [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery) and the [GPU-plugin](https://github.com/intel/intel-device-plugins-for-kubernetes/blob/main/cmd/gpu_plugin/README.md).

To begin with, it will help a lot if you have been successful already using the GPU-plugin with some deployments. That means your HW and cluster is most likely fine with GAS also.

## GPU-plugin
Resource management enabled version of the GPU-plugin is currently necessary for running GAS. The resource management enabled GPU-plugin version can read the necessary annotations of the PODs, and without those annotations, GPU allocations will not work correctly. A copy of the plugin deployment kustomization can be found from [docs/gpu_plugin](./gpu_plugin). It can be deployed simply by issuing:
```
kubectl apply -k docs/gpu_plugin/overlays/fractional_resources
```

The GPU plugin initcontainer needs to be used in order to get the extended resources created with NFD. It is deployed by the kustomization base. The initcontainer installs the required NFD-hook into the host system.

## NFD
Basically all versions starting with [v0.6.0](https://github.com/kubernetes-sigs/node-feature-discovery/releases/tag/v0.6.0) should work. You can use it to publish the GPU extended resources and GPU-related labels printed by the hook installed by the GPU-plugin initcontainer.

For picking up the labels printed by the hook installed by the GPU-plugin initcontainer, deploy nfd master with this kind of command in its yaml:
```
command: ["nfd-master", "--resource-labels=gpu.intel.com/memory.max,gpu.intel.com/millicores,gpu.intel.com/tiles", "--extra-label-ns=gpu.intel.com"]
```

The above would promote three labels, "memory.max", "millicores" and "tiles" to extended resources of the node that produces the labels.

If you want to enable i915 capability scanning, the nfd worker needs to read debugfs, and therefore it needs to run as privileged, like this:
```
          securityContext:
            runAsNonRoot: null
            # Adding GPU info labels needs debugfs "915_capabilities" access
            # (can't just have mount for that specific file because all hosts don't have i915)
            runAsUser: 0
```

In order to allow NFD to create extended resource, you will have to give it RBAC-rule to access nodes/status, like:
```
rules:
- apiGroups:
  - ""
  resources:
  - nodes
# when using command line flag --resource-labels to create extended resources
# you will need to uncomment "- nodes/status"
  - nodes/status
```

A simple example of non-root NFD deployment kustomization can be found from [docs/nfd](./nfd). You can deploy it by running

```
kubectl apply -k docs/nfd
```

## Cluster nodes

You need some i915 GPUs in the nodes. Internal GPUs work fine for testing GAS, most recent NUCs are good enough.

## PODs

Your PODs then, needs to ask for some GPU-resources. Like this:
```
        resources:
          limits:
            gpu.intel.com/i915: 1
            gpu.intel.com/millicores: 10
            gpu.intel.com/memory.max: 10M
```

Or like this for tiles:
```
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

You can use POD-annotations in your POD-templates to list the GPU names which you allow, or deny for your deployment. The values for the annotations are comma separated value lists of the form "card0,card1,card2", and the names of the annotations are:

- `gas-allow`
- `gas-deny`

Note that the feature is disabled by default. You need to enable allowlist and/or denylist via command line flags.

## PackResource

In some use cases, GPU cards allocated for containers in one pod need to be the same card, otherwise exceptions or errors(for example: gpu hang) may occur after deployment. POD-annotations gas-container-cards expected is "cardx,cardx|cardx,cardx", "x" is the same ID.

Note that the feature is disabled by default. You need to enable packResource via command line flags.

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