# Cluster API deployment - Docker provider (for local testing/development only)

** This guide is meant for local testing/development only, this is not meant for production usage.**

For the deployment using a generic provider, please refer to [Cluster API deployment - Generic provider](capi.md).

## Requirements

- A management cluster provisioned in your infrastructure of choice and the relative tooling.
  See [Cluster API Quickstart](https://cluster-api.sigs.k8s.io/user/quick-start.html).
- Run Kubernetes v1.22 or greater (tested on Kubernetes v1.25).
- Docker (tested on Docker version 20.10.22)
- Kind (tested on Kind version 0.17.0)

## Provision clusters with TAS installed using Cluster API

We will provision a KinD cluster with the TAS installed using Cluster API.

1. Run the following to set up a KinD cluster for CAPD:

```bash
cat > kind-cluster-with-extramounts.yaml <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
    - hostPath: /var/run/docker.sock
      containerPath: /var/run/docker.sock
EOF
```

2. Enable the `CLUSTER_TOPOLOGY` and `EXP_CLUSTER_RESOURCE_SET` feature gates:

```bash
export CLUSTER_TOPOLOGY=true
export EXP_CLUSTER_RESOURCE_SET=true
```

3. Initialize the management cluster:

```bash
clusterctl init --infrastructure docker
```

Run the following to generate the default cluster manifests:

```bash
clusterctl generate cluster capi-quickstart --flavor development \
  --kubernetes-version v1.25.0 \
  --control-plane-machine-count=3 \
  --worker-machine-count=3 \
  > capi-quickstart.yaml
```

If Kind was running correctly, and the Docker provider was initialized with the previous command, the command will return nothing to indicate success.

4. Merge the contents of the resources provided in `../shared/cluster-patch.yaml`, `kubeadmcontrolplanetemplate-patch.yaml` and `clusterclass-patch.yaml` with
   the resources contained in `capi-quickstart.yaml`.

The new config will:
- Configure TLS certificates for the extender
- Change the `dnsPolicy` of the scheduler to `ClusterFirstWithHostNet`
- Place `KubeSchedulerConfiguration` into control plane nodes and pass the relative CLI flag to the scheduler.
- Change the behavior of the pre-existing patch application of `/spec/template/spec/kubeadmConfigSpec/files` in `ClusterClass`
  such that our new patch is not ignored/overwritten. For some more clarification on this, see [this issue](https://github.com/kubernetes-sigs/cluster-api/pull/7630).
- Add the necessary labels for ClusterResourceSet to take effect in the workload cluster.

Therefore, we will:
- Merge the contents of file `kubeadmcontrolplanetemplate-patch.yaml` into the KubeadmControlPlaneTemplate resource of capi-quickstart.yaml.
- Replace entirely the KubeadmControlPlaneTemplate patch item with `path` `/spec/template/spec/kubeadmConfigSpec/files` with the item present in file `clusterclass-patch.yaml`.
- Add the necessary labels to the Cluster resource of capi-quickstart.yaml.

To do this, we provide some quick `yq` commands to automate the process, but you can also merge the files manually.

Patch the KubeadmControlPlaneTemplate resource by merging the contents of `kubeadmcontrolplanetemplate-patch.yaml` with the one contained in `capi-quickstart.yaml`:
```bash
# Extract KubeadmControlPlaneTemplate
yq e '. | select(.kind == "KubeadmControlPlaneTemplate")' capi-quickstart.yaml > kubeadmcontrolplanetemplate.yaml
# Merge patch
yq eval-all '. as $item ireduce ({}; . *+ $item)' kubeadmcontrolplanetemplate.yaml kubeadmcontrolplanetemplate-patch.yaml > final-kubeadmcontrolplanetemplate.yaml
# Replace the original KubeadmControlPlaneTemplate with the patched one
export KCPT_FINAL=$(<final-kubeadmcontrolplanetemplate.yaml)
yq -i '. | select(.kind == "KubeadmControlPlaneTemplate") = env(KCPT_FINAL)' capi-quickstart.yaml
```

Modify the ClusterClass patches to allow our patch to be applied:

```bash
# Extract ClusterClass
yq e '. | select(.kind == "ClusterClass")' capi-quickstart.yaml > clusterclass.yaml
export CC_PATCH=$(<clusterclass-patch.yaml)
# Replace the original ClusterClass patch with the new one
yq '(.spec.patches[].definitions[].jsonPatches[] | select(.path == "/spec/template/spec/kubeadmConfigSpec/files")) = env(CC_PATCH)' clusterclass.yaml > final-clusterclass.yaml
# Replace the ClusterClass in capi-quickstart.yaml with the new one
export CC_FINAL=$(<final-clusterclass.yaml)
yq -i '. | select(.kind == "ClusterClass") = env(CC_FINAL)' capi-quickstart.yaml
```

Add the necessary labels to the Cluster resource:

```bash
# Extract Cluster
yq e '. | select(.kind == "Cluster")' capi-quickstart.yaml > cluster.yaml
yq -i eval-all '. as $item ireduce ({}; . *+ $item)' cluster.yaml ../shared/cluster-patch.yaml
```

you should end up with something like [this](sample-capi-manifests.yaml).

5. You will need to prepare the Helm Charts of the various components and join the TAS manifests together for convenience:

First, under `telemetry-aware-scheduling/deploy/charts` tweak the charts if you need (e.g.
additional metric scraping configurations), then render the charts:

```bash
helm template ../../charts/prometheus_node_exporter_helm_chart/ > prometheus-node-exporter.yaml
helm template ../../charts/prometheus_helm_chart/ > prometheus.yaml
helm template ../../charts/prometheus_custom_metrics_helm_chart > prometheus-custom-metrics.yaml
```

You need to add namespaces resources, else resource application will fail. Prepend the following to `prometheus.yaml`:

```bash
kind: Namespace
apiVersion: v1
metadata:
  name: monitoring
  labels:
    name: monitoring
````

Prepend the following to `prometheus-custom-metrics.yaml`:
```bash
kind: Namespace
apiVersion: v1
metadata:
  name: custom-metrics
  labels:
    name: custom-metrics
```

The custom metrics adapter and the TAS deployment require TLS to be configured with a certificate and key.
Information on how to generate correctly signed certs in kubernetes can be found [here](https://github.com/kubernetes-sigs/apiserver-builder-alpha/blob/master/docs/concepts/auth.md).
Files ``serving-ca.crt`` and ``serving-ca.key`` should be in the current working directory.

Run the following:

```bash
kubectl -n custom-metrics create secret tls cm-adapter-serving-certs --cert=serving-ca.crt --key=serving-ca.key -oyaml --dry-run=client > custom-metrics-tls-secret.yaml
kubectl -n default create secret tls extender-secret --cert=serving-ca.crt --key=serving-ca.key -oyaml --dry-run=client > tas-tls-secret.yaml
```

**Attention: Don't commit the TLS certificate and private key to any Git repo as it is considered bad security practice! Make sure to wipe them off your workstation after applying the relative Secrets to your cluster.**

You also need the TAS manifests (Deployment, Policy CRD and RBAC accounts) and the extender's "configmapgetter"
ClusterRole. We will join the TAS manifests together, so we can have a single ConfigMap for convenience:

```bash
yq '.' ../tas-*.yaml > tas.yaml
```

6. Create and apply the ConfigMaps

```bash
kubectl create configmap custom-metrics-tls-secret-configmap --from-file=./custom-metrics-tls-secret.yaml -o yaml --dry-run=client > custom-metrics-tls-secret-configmap.yaml
kubectl create configmap custom-metrics-configmap --from-file=./prometheus-custom-metrics.yaml -o yaml --dry-run=client > custom-metrics-configmap.yaml
kubectl create configmap prometheus-configmap --from-file=./prometheus.yaml -o yaml --dry-run=client > prometheus-configmap.yaml
kubectl create configmap prometheus-node-exporter-configmap --from-file=./prometheus-node-exporter.yaml -o yaml --dry-run=client > prometheus-node-exporter-configmap.yaml
kubectl create configmap tas-configmap --from-file=./tas.yaml -o yaml --dry-run=client > tas-configmap.yaml
kubectl create configmap tas-tls-secret-configmap --from-file=./tas-tls-secret.yaml -o yaml --dry-run=client > tas-tls-secret-configmap.yaml
kubectl create configmap extender-configmap --from-file=../extender-configuration/configmap-getter.yaml -o yaml --dry-run=client > extender-configmap.yaml
kubectl create configmap calico-configmap --from-file=../shared/calico-configmap.yaml -o yaml --dry-run=client > calico-configmap.yaml
```

Apply to the management cluster:

```bash
kubectl apply -f '*-configmap.yaml'
```

7. Apply the ClusterResourceSets

ClusterResourceSets resources are already given to you in `../shared/clusterresourcesets.yaml`.
Apply them to the management cluster with `kubectl apply -f ../shared/clusterresourcesets.yaml`

8. Apply the cluster manifests

Finally, you can apply your manifests `kubectl apply -f capi-quickstart.yaml`.
The Telemetry Aware Scheduler will be running on your new cluster. You can connect to the workload cluster by
exporting its kubeconfig:

```bash
clusterctl get kubeconfig capi-quickstart > capi-quickstart.kubeconfig
```

Then, specifically for the CAPD docker, point the kubeconfig to the correct address of the HAProxy container:

```bash
sed -i -e "s/server:.*/server: https:\/\/$(docker port capi-quickstart-lb 6443/tcp | sed "s/0.0.0.0/127.0.0.1/")/g" ./capi-quickstart.kubeconfig
```

You can test if the scheduler actually works by following this guide:
[Health Metric Example](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/docs/health-metric-example.md)