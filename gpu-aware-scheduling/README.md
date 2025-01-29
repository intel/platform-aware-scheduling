# PROJECT NOT UNDER ACTIVE MANAGEMENT
This project will no longer be maintained by Intel.  
Intel has ceased development and contributions including, but not limited to, maintenance, bug fixes, new releases, or updates, to this project.  
Intel no longer accepts patches to this project.  
If you have an ongoing need to use this project, are interested in independently developing it, or would like to maintain patches for the open source software community, please create your own fork of this project.  

# GPU Aware Scheduling
GPU Aware Scheduling (GAS) allows using GPU resources such as memory amount for scheduling decisions in Kubernetes. It is used to optimize scheduling decisions when the POD resource requirements include the use of several GPUs or fragments of GPUs on a node, instead of traditionally mapping a whole GPU to a pod.

GPU Aware Scheduling is deployed in a single pod on a Kubernetes Cluster. It works with discrete (including Intel® Data Center GPU Flex Series) and integrated Intel GPU devices.

### GPU Aware Scheduler Extender
GPU Aware Scheduler Extender is contacted by the generic Kubernetes Scheduler every time it needs to make a scheduling decision for a pod which requests the `gpu.intel.com/i915` resource.
The extender checks if there are other `gpu.intel.com/`-prefixed resources associated with the workload.
If so, it inspects the respective node resources and filters out those nodes where the POD will not really fit.
This is implemented and configured as a [Kubernetes Scheduler Extender.](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/#cluster-level-extended-resources)

The typical use-case which GAS solves can be described with the following imaginary setup.
1) Node has two GPUs, each having 8GB of GPU memory. The node advertises 16GB of GPU memory as a kubernetes extended resource.
2) POD instances need 5GB of GPU memory each
3) A replicaset of 3 is created for said PODs, totaling a need for 3*5GB = 15GB of GPU memory.
4) Kubernetes Scheduler, if left to its own decision making, can place all the PODs on this one node with only 2 GPUs, since it only considers the memory amount of 16GB. However to be able to do such a deployment and run the PODs successfully, the last instance would need to allocate the GPU memory from two of the GPUs.
5) GAS solves this issue by keeping book of the individual GPU memory amount. After two PODs have been deployed on the node, both GPUs have 3GB of free memory left. When GAS sees that the need for memory is 5GB but none of the GPUs in that node have as much (even though combined there is still 6GB left) it will filter out that node from the list of the nodes k8s scheduler proposed. The POD will not be deployed on that node.

GAS tries to be agnostic about resource types. It doesn't try to have an understanding of the meaning of the resources, they are just numbers to it, which it identifies from other Kubernetes extended resources with the prefix `gpu.intel.com/`. The only resources treated differently is the GPU-plugin `i915` and `xe` resources, which are considered to describe "from how many GPUs the GPU-resources for the POD should be evenly consumed". That is, if each GPU has e.g. capacity of 1000 `gpu.intel.com/millicores", and POD spec has a limit for two (2) `gpu.intel.com/i915` and 2000 "gpu.intel.com/millicores`, that POD will consume 1000 millicores from two GPUs, totaling 2000 millicores. After GAS has calculated the resource requirement per GPU by dividing the extended resource numbers with the number of requested `i915` or `xe`, deploying the POD to a node is only allowed if there are enough resources in the node to satisfy fully the per-GPU resource requirement in as many GPUs as requested in `i915` or `xe` resource. Typical PODs use just one `i915` or `xe` and consume resources from only a single GPU. Note that a Pod must only request either `i915` or `xe` resources, but never both.

GAS heavily utilizes annotations. It itself annotates PODs after making filtering decisions on them, with a precise timestamp at annotation named "gas-ts". The timestamp can then be used for figuring out the time-order of the GAS-made scheduling decision for example during the GPU-plugin resource allocation phase, if the GPU-plugin wants to know the order of GPU-resource consuming POD deploying inside the node. Another annotation which GAS adds is "gas-container-cards". It will have the names of the cards selected for the containers. Containers are separated by "|", and card names are separated by ",". Thus a two-container POD in which both containers use 2 GPUs, could get an annotation "card0,card1|card2,card3". These annotations are then consumed by the Intel GPU device plugin.

Along with the "gas-container-cards" annotation there can be a "gas-container-tiles" annotation. This annotation is created when a container requests tile resources (`gpu.intel.com/tiles`). The gtX marking for tiles follows the sysfs entries under `/sys/class/drm/cardX/gt/` where the `cardX` can be any card in the system. "gas-container-tiles" annotation marks the card+tile combos assigned to each container. For example a two container pod's annotation could be "card0:gt0+gt1|card0:gt2+gt3" where each container gets two tiles from the same GPU. The tile annotation is then converted to corresponding environment variables by the GPU plugin.

GAS also expects labels to be in place for the nodes, in order to be able to keep book of the cluster GPU resource status. Nodes with GPUs shall be labeled with label name `gpu.intel.com/cards` and value shall be in form "card0.card1.card2.card3"... where the card names match with the intel GPUs which are currently found under `/sys/class/drm` folder, and the dot serves as separator. GAS expects all GPUs of the same node to be homogeneous in their resource capacity, and calculates the GPU extended resource capacity as evenly distributed to the GPUs listed by that label.

## Usage with NFD and the GPU-plugin
A worked example for GAS is available [here](docs/usage.md)

### Quick set up
The deploy folder has all of the yaml files necessary to get GPU Aware Scheduling running in a Kubernetes cluster. Some additional steps are required to configure the generic scheduler.

#### Extender configuration
Recommended way: use the [configurator go tool](../configurator/README.md) to do the configuration.

Alternatively you can use the `configure-scheduler.sh` script and instructions from the
[Telemetry Aware Scheduling](../telemetry-aware-scheduling/README.md#Extender-configuration) and
adapt those instructions to use GPU Aware Scheduling configurations, which can be found in the
[deploy/extender-configuration](deploy/extender-configuration) folder.

#### Deploy GAS
Note: if you used the configurator instructions, you are probably already done and you can continue verifying the setup.

GAS has been tested with Kubernetes 1.24. A yaml file for GAS is contained in the deploy folder
along with its service and RBAC roles and permissions.

A secret called `extender-secret` will need to be created with the cert and key for the TLS endpoint.
If you name the secret differently, remember to fix the [deployment file](deploy/gas-deployment.yaml) respectively before deploying GAS.

The secret can be created with command:

```bash
kubectl create secret tls extender-secret --cert /etc/kubernetes/<PATH_TO_CERT> --key /etc/kubernetes/<PATH_TO_KEY>
```

Replace <PATH_TO_CERT> and <PATH_TO_KEY> with the names your cluster has, here is the same command
 with the default values:

```bash
kubectl create secret tls extender-secret --cert /etc/kubernetes/pki/ca.crt --key /etc/kubernetes/pki/ca.key
```

Note, you might need privileges to access default location of these files, use `sudo <same command>` then in this case.

The `deploy`-folder has the necessary scripts for deploying GAS. You can simply deploy by running:

```bash
kubectl apply -f deploy/
```

After this is run GAS should be operable in the cluster and should be visible after running `kubectl get pods`.

Remember to run the `configure-scheduler.sh` script, or perform similar actions in your cluster if the script does not work in your environment directly.

#### Build GAS locally

GPU Aware Scheduling uses go modules. It requires Go 1.18 with modules enabled for building.
To build GAS locally on your host:

```bash
make build
```

You can also build inside docker, which creates the container:

```bash
make image
```

To deploy locally built GAS container image, just change the [deployment YAML](deploy/gas-deployment.yaml) and deploy normally as if it was pre-built image, see above.

### Configuration flags
The below flags can be passed to the binaries at run time.

#### GAS Scheduler Extender
name |type | description| usage | default|
-----|------|-----|-------|-----|
|kubeConfig| string |location of kubernetes configuration file | --kubeConfig /root/filename|~/.kube/config
|port| int | port number on which the scheduler extender will listen| --port 32000 | 9001
|cert| string | location of the cert file for the TLS endpoint | --cert=/root/cert.txt| /etc/kubernetes/pki/ca.crt
|key| string | location of the key file for the TLS endpoint| --key=/root/key.txt | /etc/kubernetes/pki/ca.key
|cacert| string | location of the ca certificate for the TLS endpoint| --cacert=/root/cacert.txt | /etc/kubernetes/pki/ca.crt
|enableAllowlist| bool | enable POD-annotation based GPU allowlist feature | --enableAllowlist| false
|enableDenylist| bool | enable POD-annotation based GPU denylist feature | --enableDenylist| false
|balancedResource| string | enable named resource balancing between GPUs | --balancedResource| ""
|burst| int | burst value to use with kube client | --burst| 10
|qps| int | qps value to use with kube client | --qps| 5

Some features are based on the labels put onto pods, for full features list see [usage doc](docs/usage.md)

#### Balanced resource (optional)
GAS can be configured to balance named resources so that the resource requests are distributed as evenly as possible between the GPUs. For example if the balanced resource is set to "tiles" and the containers request 1 tile each, the first container could get tile from "card0", the second from "card1", the third again from "card0" and so on.

## Adding the resource to make a deployment use GAS Scheduler Extender

For example, in a deployment file:
```yaml
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
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            gpu.intel.com/i915: 1
```

There is one change to the yaml here:
- A resources/limits entry requesting the resource `gpu.intel.com/i915` will make GAS take part in scheduling such deployment. If this resource is not requested, GAS will not be used during scheduling of the pod. Note: the `gpu.intel.com/xe` resource is also supported and Pods using it will also be scheduled through GAS.

### Unsupported use-cases

Topology Manager and GAS card selections can conflict. Using both at the same time is not supported. You may use topology manager without GAS.
Using [CRI-RM](https://github.com/intel/cri-resource-manager)
[topology aware policy](https://intel.github.io/cri-resource-manager/stable/docs/policy/topology-aware.html#topology-aware-policy)
is encouraged instead, it works together with GAS.

Selecting deployment node directly in POD spec [nodeName](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodename) bypasses the scheduler and therefore also GAS. This is obviously a use-case which can't be supported by GAS, so don't use that mechanism, if you want to run the scheduler and GAS.

### Security
GAS Scheduler Extender is set up to use in-Cluster config in order to access the Kubernetes API Server. When deployed inside the cluster this along with RBAC controls configured in the installation guide, will give it access to the required resources.

Additionally GAS Scheduler Extender listens on a TLS endpoint which requires a cert and a key to be supplied.
These are passed to the executable using command line flags. In the provided deployment these certs are added in a Kubernetes secret which is mounted in the pod and passed as flags to the executable from there.

## License

[Apache License, Version 2.0](./LICENSE). All of the source code required to build the GPU Aware Scheduling is available under Open Source
licenses. The source code files identify external Go modules used. The binary is distributed as a container image on
[DockerHub](https://hub.docker.com/r/intel/gpu-extender). The container image contains license texts under folder `/licenses`.

## Communication and contribution

Report a bug by [filing a new issue](https://github.com/intel/platform-aware-scheduling/issues).

Contribute by [opening a pull request](https://github.com/intel/platform-aware-scheduling/pulls).

Learn [about pull requests](https://help.github.com/articles/using-pull-requests/).

**Reporting a Potential Security Vulnerability:** If you have discovered potential security vulnerability in GAS, please send an e-mail to secure@intel.com. For issues related to Intel Products, please visit [Intel Security Center](https://security-center.intel.com).

It is important to include the following details:

- The projects and versions affected
- Detailed description of the vulnerability
- Information on known exploits

Vulnerability information is extremely sensitive. Please encrypt all security vulnerability reports using our [PGP key](https://www.intel.com/content/www/us/en/security-center/pgp-public-key.html).

A member of the Intel Product Security Team will review your e-mail and contact you to collaborate on resolving the issue. For more information on how Intel works to resolve security issues, see: [vulnerability handling guidelines](https://www.intel.com/content/www/us/en/security-center/vulnerability-handling-guidelines.html).

