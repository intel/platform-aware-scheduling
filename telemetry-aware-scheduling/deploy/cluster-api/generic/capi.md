# Cluster API deployment - Generic provider

** This guide is meant for local testing/development only, this is not meant for production usage.**

## Requirements

- A management cluster provisioned in your infrastructure of choice and the relative tooling.
  See [Cluster API Quickstart](https://cluster-api.sigs.k8s.io/user/quick-start.html).
- Run Kubernetes v1.22 or greater (tested on Kubernetes v1.25).

## Provision clusters with TAS installed using Cluster API

We will provision a generic cluster with the TAS installed using Cluster API. This was tested using a the GCP provider.

For the deployment using the Docker provider (local testing/development only), please refer to [Cluster API deployment - Generic provider](capi.md).
1. In your management cluster, with all your environment variables set to generate cluster definitions, run for example:

```bash
clusterctl generate cluster scheduling-dev-wkld \
  --kubernetes-version v1.25.0 \
  --control-plane-machine-count=1 \
  --worker-machine-count=3 \
  > capi-quickstart.yaml
```

Be aware that you will need to install a CNI such as Calico before the cluster will be usable. 
Calico works for the great majority of providers, so all configurations have been provided for your convenience, i.e ClusterResourceSet, CRS label in Cluster and CRS ConfigMap). 
For more information, see [Deploy a CNI solution](https://cluster-api.sigs.k8s.io/user/quick-start.html#deploy-a-cni-solution) in the CAPI quickstart.

2. Merge the contents of the resources provided in `../shared/cluster-patch.yaml` and `kubeadmcontrolplane-patch.yaml` with
   `capi-quickstart.yaml`.

If you move `KubeadmControlPlane` in its own file, you can use the convenient `yq` utility:

> Note that if you are already using patches, `directory: /tmp/kubeadm/patches` must coincide, else the property will be
> overwritten.

```bash
yq eval-all '. as $item ireduce ({}; . *+ $item)' your-own-kubeadmcontrolplane.yaml kubeadmcontrolplane-patch.yaml > final-kubeadmcontrolplane.yaml
```

The new config will:
- Configure TLS certificates for the extender
- Change the `dnsPolicy` of the scheduler to `ClusterFirstWithHostNet`
- Place `KubeSchedulerConfiguration` into control plane nodes and pass the relative CLI flag to the scheduler.

3. You will need to prepare the Helm Charts of the various components and join the TAS manifests together for convenience:

First, under `telemetry-aware-scheduling/deploy/charts` tweak the charts if you need (e.g.
additional metric scraping configurations), then render the charts:

```bash
helm template ../charts/prometheus_node_exporter_helm_chart/ > prometheus-node-exporter.yaml
helm template ../charts/prometheus_helm_chart/ > prometheus.yaml
helm template ../charts/prometheus_custom_metrics_helm_chart > prometheus-custom-metrics.yaml
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

4. Create and apply the ConfigMaps

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

5. Apply the ClusterResourceSets

ClusterResourceSets resources are already given to you in `../shared/clusterresourcesets.yaml`.
Apply them to the management cluster with `kubectl apply -f ./shared/clusterresourcesets.yaml`

6. Apply the cluster manifests

Finally, you can apply your manifests `kubectl apply -f capi-quickstart.yaml`.
The Telemetry Aware Scheduler will be running on your new cluster.

You can test if the scheduler actually works by following this guide:
[Health Metric Example](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/docs/health-metric-example.md)