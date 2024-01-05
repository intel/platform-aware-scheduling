## Power Driven Scheduling and Scaling with CPU telemetry in Kubernetes 

**NOTICE: This is a demo to show how you can leverage scheduling and automatic scaling of workloads using TAS. This is not ready for production usage.**

This guide will demonstrate intelligent scheduling and automatic scaling of power sensitive workloads in Kubernetes. Fine grained platform automation based on power can be leveraged to reduce resource consumption overall and increase individual workload performance by reducing misplacement of power-hungry workloads.

Telemetry Aware Scheduling, a scheduling extension, and the Kubernetes native Horizontal Pod Autoscaler are used to enable cluster automation based on real time information about the current state of power usage on the node.

The power metrics used to drive placement and scaling decisions derive from [Intel's Running Average Power Limit (RAPL)](https://01.org/blogs/2014/running-average-power-limit-%E2%80%93-rapl). [collectd](https://github.com/collectd) is used to gather the metrics and expose them to [Prometheus](https://github.com/prometheus/prometheus) which makes them available inside the cluster using the [Prometheus Adapter](https://github.com/DirectXMan12/k8s-prometheus-adapter)


### 0: Prerequisites and environment set up

1) A pre-existing Kubernetes 1.18 cluster installed using the Intel [Bare Metal Reference Architecture](https://github.com/intel/container-experience-kits) (BMRA). A number of features of BMRA -  including Telemetry Aware Scheduling, the custom metrics pipeline, local Docker registry, and pre-configured control plane - are relied on in the steps that follow.

2) A user with sufficient permissions logged in to the Kubernetes master node.

3) Clone and enter repo

``git clone https://github.com/intel/platform-aware-scheduling/ && cd platform-aware-scheduling/telemetry-aware-scheduling/docs/power``

4) Set the power directory (which contains this Readme) as an environment variable.

``export POW_DIR=$PWD``


### 1: Deploy collectd on Kubernetes
Collectd can now be deployed on our Kubernetes cluster. Note that the collectd image is configured using a configmap and can be reconfigured by changing that file - located in `collectd/configmap.yaml`. In our default configuration collectd will only export stats on the node CPU package power. This is powered by the [Intel Comms Power Management collectd plugin](https://github.com/intel/CommsPowerManagement/tree/master/telemetry). The collectd [intel/observability-collectd image](https://hub.docker.com/r/intel/observability-collectd) will need an extra python script for reading and exporting power values which can be grabbed with a curl command and will need to be added as a configmap.

Deploy collectd using:

1) ``curl https://raw.githubusercontent.com/intel/CommsPowerManagement/master/telemetry/pkgpower.py -o $POW_DIR/collectd/pkgpower.py && kubectl create configmap pkgpower --from-file $POW_DIR/collectd/pkgpower.py --namespace monitoring``

2) ``kubectl apply -f $POW_DIR/collectd/daemonset.yaml && kubectl apply -f $POW_DIR/collectd/configmap.yaml && kubectl apply -f $POW_DIR/collectd/service.yaml``

The agent should soon be up and running on each node in the cluster. This can be checked by running: 

`kubectl get pods -nmonitoring -lapp.kubernetes.io/name=collectd`

Ensure the collectd pod(s) - one per each node in your cluster, have been set-up properly. Using `kubectl logs  collectd-**** -n monitoring` open the pod logs and ensure there are no errors/warning and that you see something similar to:

```
plugin_load: plugin "python" successfully loaded.
plugin_load: plugin "write_prometheus" successfully loaded.
write_prometheus plugin: Listening on [::]:9103.
write_prometheus plugin: MHD_OPTION_EXTERNAL_LOGGER is not the first option specified for the daemon. Some messages may be printed by the standard MHD logger.

Initialization complete, entering read-loop.
```

If you encounter errors like the one below in your collectd pod, check read permissions for the file in question. From running this demo so far, it seems like collectd requires at least read permissions for the ***'others'*** permission group in order to access sys energy files.

```
Unhandled python exception in read callback: PermissionError: [Errno 13] Permission denied: '/sys/devices/virtual/powercap/intel-rapl/intel-rapl:1/energy_uj'
read-function of plugin `python.pkgpower' failed. Will suspend it for 20.000 seconds.
```

Additionally we can check that collectd is indeed serving metrics by running `collectd_ip=$(kubectl get svc -n monitoring -o json | jq -r '.items[] | select(.metadata.name=="collectd").spec.clusterIP'); curl -x "" $collectd_ip:9103/metrics`. The output should look similar to the lines  below:

```
# HELP collectd_package_0_TDP_power_power write_prometheus plugin: 'package_0_TDP_power' Type: 'power', Dstype: 'gauge', Dsname: 'value'
# TYPE collectd_package_0_TDP_power_power gauge
collectd_package_0_TDP_power_power{instance="collectd-llnxh"} 140 1686148275145
# HELP collectd_package_0_power_power write_prometheus plugin: 'package_0_power' Type: 'power', Dstype: 'gauge', Dsname: 'value'
# TYPE collectd_package_0_power_power gauge
collectd_package_0_power_power{instance="collectd-llnxh"} 127.249769171414 1686148275145
# HELP collectd_package_1_TDP_power_power write_prometheus plugin: 'package_1_TDP_power' Type: 'power', Dstype: 'gauge', Dsname: 'value'
# TYPE collectd_package_1_TDP_power_power gauge
collectd_package_1_TDP_power_power{instance="collectd-llnxh"} 140 1686148275145
# HELP collectd_package_1_power_power write_prometheus plugin: 'package_1_power' Type: 'power', Dstype: 'gauge', Dsname: 'value'
# TYPE collectd_package_1_power_power gauge
collectd_package_1_power_power{instance="collectd-llnxh"} 117.843289048237 1686148275145

```

### 2: Install Kube State Metrics

Kube State Metrics reads information from a Kubernetes cluster and makes it available to Prometheus. In our use case we're looking for basic information about the pods currently running on the cluster. Kube state metrics can be installed using helm, the Kubernetes package manager, which comes preinstalled in the BMRA.
 
``
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts && helm install "master" prometheus-community/kube-state-metrics
``

We can check to see that its running with:

``
kubectl get pods -l app.kubernetes.io/name=kube-state-metrics
``

### 3: Configure Prometheus and the Prometheus adapter

Now that we have our metrics collections agents up and running, Prometheus and Prometheus Adapter (it  makes Prometheus metrics available inside the cluster to Telemetry Aware Scheduling) need to be configured to scrape metrics from collectd and Kube State Metrics and to calculate the compound power metrics linking pods to power usage.


#### 3.1: Manually set-up and install new configuration

The above command creates a new configuration and reboots Prometheus and the Prometheus Adapter. Collectd power metrics should now be available in the Prometheus database and, more importantly, inside the Kubernetes cluster itself. 

``kubectl apply -f $POW_DIR/prometheus/prometheus-config.yaml && kubectl delete pods -nmonitoring -lapp=prometheus -lcomponent=server && kubectl apply -f $POW_DIR/prometheus/custom-metrics-config.yml && kubectl delete pods -n custom-metrics -lapp=custom-metrics-apiserver``


#### 3.2: Helm and updating an existing configuration

#### 3.2.1: Prometheus

To add a new metrics into Prometheus, update [prometheus-config-map.yaml](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/deploy/charts/prometheus_helm_chart/templates/prometheus-config-map.yaml) in a similar manner to the below:

1. Create a metric and add it to a Prometheus rule file
```
  recording_rules.yml: |
   groups:
   - name: k8s rules
     rules:
      - record: node_package_power_avg
        expr: 100 * ((collectd_package_0_power_power/collectd_package_0_TDP_power_power) + (collectd_package_1_power_power/ collectd_package_1_TDP_power_power))/2
      - record: node_package_power_per_pod
        expr:  kube_pod_info * on (node) group_left node_package_power_avg
```

2. Add the rule file to Prometheus (if adding a completely new rule file). If the rule file has already been done (i.e.  ***/etc/prometheus/prometheus.rules***) move to step 3
```
    rule_files:
       - /etc/prometheus/prometheus.rules
       - recording_rules.yml
```
3. Inform prometheus to scrape kube-state-metrics and collectd and how to do do it

```

      - job_name: 'kube-state-metrics'
        static_configs:
        - targets: ['master-kube-state-metrics.default.svc.cluster.local:8080']

      - job_name: 'collectd'
        scheme: http
        kubernetes_sd_configs:
        - role: endpoints
        relabel_configs:
        - source_labels: [__address__]
          regex: ^(.*):\d+$
          target_label: __address__
          replacement: $1:9103
        - target_label: __scheme__
          replacement: http
        - source_labels: [__meta_kubernetes_pod_node_name]
          target_label: instance
        metric_relabel_configs:
        - source_labels: [instance]
          target_label: node

```

To check if the new metrics were scraped correctly you can look for the following in the Prometheus UI:
* In the "Targets"/ "Service Discovery" sections the "kube-state-metrics" and "collectd" items should be present and should be "UP" 
* In the main search page look for the "node_package_power_avg" and "node_package_power_per_pod". Each of these queries should return and non-empty answer


4. Upgrade the HELM chart & restart the pods

``helm upgrade  prometheus ../../../telemetry-aware-scheduling/deploy/charts/prometheus_helm_chart/ &&  kubectl delete pods -nmonitoring -lapp=prometheus-server``

***The name of the helm chart and the path to charts/prometheus_helm_chart might differ depending on your installation of Prometheus (what name you gave to the chart) and your current path.***

#### 3.2.2: Prometheus Adapter

1. Query & process the metric before exposing it

The newly created metrics now need to be exported from Prometheus to the Telemetry-Aware scheduler and to do so we need add two new rules in [custom-metrics-config-map.yaml](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/deploy/charts/prometheus_custom_metrics_helm_chart/templates/custom-metrics-config-map.yaml). 

The config currently present in the [repo](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/deploy/charts/prometheus_custom_metrics_helm_chart/templates/custom-metrics-config-map.yaml)  will expose these metrics by default, but if the metrics are not showing (see commands below) you can try adding the following ( the first item is responsible for fetching the "node_package_power_avg" metric, whereas the second is for "node_package_power_per_pod"):

```
    - seriesQuery: '{__name__=~"^node_.*"}'
      resources:
        overrides:
          instance:
            resource: node
      name:
        matches: ^node_(.*)
      metricsQuery: <<.Series>>
    - seriesQuery: '{__name__=~"^node_.*",pod!=""}'
      resources:
        template: <<.Resource>>
      metricsQuery: <<.Series>>

```

***Details about the rules and the schema are available [here](https://github.com/kubernetes-sigs/prometheus-adapter/blob/master/docs/config.md)***

2. Upgrade the HELM chart & restart the pods

``helm upgrade prometheus-adapter ../../../telemetry-aware-scheduling/deploy/charts/prometheus_custom_metrics_helm_chart/ && kubectl delete pods -n custom-metrics -lapp=custom-metrics-apiserver``

***The name of the helm chart and the path to charts/prometheus_custom_metrics_helm_chart/ might differ depending on your installation of Prometheus (what name you have to the chart) and your current path.***


After running the above commands (section 3.1 or 3.2) Collectd power metrics should now be available in the Prometheus database and, more importantly, inside the Kubernetes cluster itself. In order to confirm the changes, we can run a raw metrics query against the Kubernetes api.

``kubectl get --raw /apis/custom.metrics.k8s.io/v1beta1/nodes/*/package_power_avg``

``kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/namespaces/default/pods/*/node_package_power_per_pod"``

This command should return some json objects containing pods and associated power metrics:


```json
  "kind": "MetricValueList",
  "apiVersion": "custom.metrics.k8s.io/v1beta1",
  "metadata": {
    "selfLink": "/apis/custom.metrics.k8s.io/v1beta1/namespaces/default/pods/%2A/node_package_power_per_pod"
  },
  "items": [
    {
      "describedObject": {
        "kind": "Pod",
        "namespace": "default",
        "name": "master-kube-state-metrics-7ddb598f6c-kfb8m",
        "apiVersion": "/v1"
      },
      "metricName": "node_package_power_per_pod",
      "timestamp": "2020-XX-XXTXX:XX:XXZ",
      "value": "41020m",
      "selector": null
    }

```
*Note* If the above command returns an error there is some issue in the metrics pipeline that requires resolution before continuing.

If the `node_package_power_per_pod` metric is visible but it doesn't have a value ("items" is an empty array like in the response below) please check both the `node_package_power_per_pod` and `package_power_avg` metrics in Prometheus UI. Assuming the collectd and kube-state-metrics pods have been set-up correctly, you should see `node_package_power_per_pod` and `package_power_avg` metrics for each node in your cluster. Expect a similar behaviour for collectd metrics like `collectd_package_*_TDP_power`.
If you notice metrics are missing for some nodes, start by investigating which nodes are affected and check logs for the pods in question.

```json
{"kind":"MetricValueList","apiVersion":"custom.metrics.k8s.io/v1beta1","metadata":{"selfLink":"/apis/custom.metrics.k8s.io/v1beta1/namespaces/default/pods/%2A/node_package_power_per_pod"},"items":[]}
```

### 4: Create TAS Telemetry Policy for power
On the Kubernetes BMRA set up Telemetry Aware Scheduling is already installed and integrated with the Kubernetes control plane. In order to allow our cluster to make scheduling decisions based on current power usage we simply need to create a telemetry policy and associate appropriate pods with that policy.

``kubectl create namespace power-demo``

then

``kubectl apply -f $POW_DIR/tas-policy.yaml``

We can see our policy by running:

``kubectl describe taspolicy power-sensitive-scheduling-policy -n power-demo``

### 5: Create horizontal autoscaler for power
The Horizontal Pod Autoscaler is an in-built part of Kubernetes which allows scaling decisions to be made based on arbitrary metrics. In this case we're going to scale our workload based on the average power usage on nodes with current instances of our workload. 

To create this Autoscaler:

``kubectl apply -f $POW_DIR/power-autoscaler.yaml``

We can see the details of our autoscaler with:

``kubectl describe hpa power-autoscaler -n power-demo``

Don't worry if the autoscaler shows errors at this point. It won't work until we've deployed an application it's interested in - based on the "scaleTargetRef" field in its specification file.

### 6: Create power sensitive application and show scheduling decisions
*Note:* Power usage is different on different systems and the policies included here make naive assumptions about available CPU cores and power scaling considerations. Our workload will look to stress 12 cpus at a time, while our autoscaler will start scaling once power usage is above 50 percent of the system's Thermal Design Power (TDP). These factors, contained in the power-hungry-application.yml ( in "args") and power-autoscaler.yml (in "averageValue") respectively, may need to be tuned on different systems.

Before deploying our workload, let's take a look at its spec to see how it integrates with Telemetry Aware Scheduling and our Horizontal Pod Autoscaler.

```
cat $POW_DIR/power-hungry-application.yaml

apiVersion: apps/v1
kind: Deployment
metadata:
  name: power-hungry-application
  labels:
    app: power-hungry-application
    telemetry-policy: power-sensitive-scheduling-policy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: power-hungry-application
  template:
    metadata:
      labels:
        telemetry-policy: power-sensitive-scheduling-policy
        app: power-hungry-application
    spec:
      containers:
      - name: stressng
        image: localhost:5000/stressng
        command: [ "/bin/bash", "-c", "--" ]
        args: ["sleep 30; stress-ng --cpu 12 --timeout 300s"]
        resources:
          limits:
            telemetry/scheduling: 1

```

The deployment starts with a single replica of our stress-ng container. The container stays dormant for 30 seconds and then begins to stress 12 cpus for five minutes after which it will finish. The container is labeled with `telemetry-policy: power-sensitive-scheduling-policy` which lets the Telemetry Aware Scheduling system know which policy it should use for placement. Additionally the `telemetry/scheduling: 1` line is what causes Kubernetes to call on TAS with the BMRA scheduling configuration.

We can create the pod with 
``kubectl apply -f $POW_DIR/power-hungry-application.yaml``

TAS will schedule it to the node with the lowest power usage, and will avoid scheduling to any node with more than 80 percent of its TDP currently in use. This can be seen in the scheduler extender logs:

```

 kubectl logs -l app=tas -n telemetry-aware-scheduling

2020/XX/XX XX:XX:XX filter request recieved
2020/XX/XX XX:XX:XX node1 package_power_avg = 72.133
2020/XX/XX XX:XX:XX node2 package_power_avg = 40.821
2020/XX/XX XX:XX:XX master1 package_power_avg = 51.774
2020/XX/XX XX:XX:XX Filtered nodes for power-sensitive-scheduling-policy : node1 master1 node2
2020/XX/XX XX:XX:XX Received prioritize request
2020/XX/XX XX:XX:XX package_power_avg for nodes:  [ node2 :40.821] [ master1 :51.774] [ node1 :72.133]
2020/XX/XX XX:XX:XX node priorities returned: [{node2 10} {master1 9} {node1 8}]

```

Once the stressng pod starts to run the horizontal pod autoscaler will notice an uptick in average power for the deployment. This will then result in a "scaling out" where an additional pod running the same worklod is created. The process will take about a minute or so and can be seen by watching the power autoscaler object:

```

watch kubectl describe hpa power-autoscaler -n power-demo

Name:                                    power-autoscaler
Namespace:                               default
Labels:                                  <none>
Annotations:                             CreationTimestamp:  Tue, 26 May 2020 13:50:05 +0100
Reference:                               Deployment/power-hungry-application
Metrics:                                 ( current / target )
  "node_package_power_per_pod" on pods:  55330m / 50
Min replicas:                            1
Max replicas:                            3
Deployment pods:                         1 current / 2 desired
Conditions:
  Type            Status  Reason              Message
  ----            ------  ------              -------
  AbleToScale     True    SucceededRescale    the HPA controller was able to update the target scale to 2
  ScalingActive   True    ValidMetricFound    the HPA was able to successfully calculate a replica count from pods
 metric node_package_power_per_pod
  ScalingLimited  False   DesiredWithinRange  the desired count is within the acceptable range

```
Another look at the Telemetry Aware Scheduling logs shows us that the newly created workload is directed to a node with lower power usage, spreading consumption across the cluster and ensuring that TDP is not reached by accidentally stacking similar workloads on the same system.

Once the scaling is complete the value of the power metric will fall as it represents an average across all workloads in the deployment. As with the first workload, the second pod will start to stress the system after thirty seconds. This will lead to another scaling event. In our autoscaler the absolute numer of replicas is limited to three, so no more scaling events will take place.

Both Telemetry Aware Scheduling and the Horizontal Pod Autoscaler apply specific telemetry driven rules to a specific set of pods. This guide leveraged Intel RAPL power metrics, but there are a multitude of different metrics available [including those for system health](https://builders.intel.com/docs/networkbuilders/closed-loop-platform-automation-service-healing-and-platform-resilience.pdf), which can be used to tailor intelligent scheduling and scaling decisions for specific workloads.

