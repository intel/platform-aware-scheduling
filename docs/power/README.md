## Power Driven Scheduling and Scaling with CPU telemetry in Kubernetes 

This guide will demonstrate intelligent scheduling and automatic scaling of power sensitive workloads in Kubernetes. Fine grained platform automation based on power can be leveraged to reduce resource consumption overall and increase individual workload performance by reducing misplacement of power-hungry workloads.

Telemetry Aware Scheduling, a scheduling extension, and the Kubernetes native Horizontal Pod Autoscaler are used to enable cluster automation based on real time information about the current state of power usage on the node.

The power metrics used to drive placement and scaling decisions derive from [Intel's Running Average Power Limit (RAPL)](https://01.org/blogs/2014/running-average-power-limit-%E2%80%93-rapl). [collectd](https://github.com/collectd) is used to gather the metrics and expose them to [Prometheus](https://github.com/prometheus/prometheus) which makes them available inside the cluster using the [Prometheus Adapter](https://github.com/DirectXMan12/k8s-prometheus-adapter)


### 0: Prerequisites and environment set up

1) A pre-existing Kubernetes 1.18 cluster installed using the Intel [Bare Metal Reference Architecture](https://github.com/intel/container-experience-kits) (BMRA). A number of features of BMRA -  including Telemetry Aware Scheduling, the custom metrics pipeline, local Docker registry, and pre-configured control plane - are relied on in the steps that follow.

2) A user with sufficient permissions logged in to the Kubernetes master node.

3) Clone and enter repo

``git clone https://github.com/intel/telemetry-aware-scheduling/ && cd telemetry-aware-scheduling/docs/power``

4) Set the power directory (which contains this Readme) as an environment variable.

``export POW_DIR=$PWD``

### 1: Build collectd and stress-ng docker images
Two new docker images are required:

1) **collectd** to read power metrics from each host in the cluster and expose them to the Prometheus metrics database.

``cd $POW_DIR/collectd && docker build . -t localhost:5000/collectdpower && docker push localhost:5000/collectdpower``

2) **stress-ng** to use as an example application for our deployment.

``cd $POW_DIR && docker build . -t localhost:5000/stressng && docker push localhost:5000/stressng``

Using the above commands both of these images will be built and then pushed to the local docker registry preparing them for deployment on Kubernetes.

### 2: Deploy collectd on Kubernetes
Collectd can now be deployed on our Kubernetes cluster. Note that the collectd image is configured using a configmap and can be reconfigured by changing that file - located in `collectd/configmap.yaml`. In our default configuration collectd will only export stats on the node CPU package power. This is powered by the [Intel Comms Power Management collectd plugin](https://github.com/intel/CommsPowerManagement/tree/master/telemetry) 

Deploy collectd using:

``kubectl apply -f $POW_DIR/collectd/daemonset.yaml && kubectl apply -f $POW_DIR/collectd/configmap.yaml && kubectl apply -f $POW_DIR/collectd/service.yaml``

The agent should soon be up and running on each node in the cluster. This can be checked by running: 

`kubectl get pods -nmonitoring -lapp.kubernetes.io/name=collectd`


Additionally we can check that collectd is indeed serving metrics by running `curl localhost:9103` on any node where the agent is running. The output should resemble the below:

```
    # HELP collectd_package_0_TDP_power_power write_prometheus plugin: 'package_0_TDP_power' Type: 'power', Dstype: 'gauge', Dsname: 'value'
    # TYPE collectd_package_0_TDP_power_power gauge
    collectd_package_0_TDP_power_power{instance="localhost"} 145 XXXXX
    # HELP collectd_package_0_power_power write_prometheus plugin: 'package_0_power' Type: 'power', Dstype: 'gauge', Dsname: 'value'
    # TYPE collectd_package_0_power_power gauge
    collectd_package_0_power_power{instance="localhost"} 24.8610455766901 XXXXX
    # HELP collectd_package_1_TDP_power_power write_prometheus plugin: 'package_1_TDP_power' Type: 'power', Dstype: 'gauge', Dsname: 'value'
    # TYPE collectd_package_1_TDP_power_power gauge
    collectd_package_1_TDP_power_power{instance="localhost"} 145 XXXXX
    # HELP collectd_package_1_power_power write_prometheus plugin: 'package_1_power' Type: 'power', Dstype: 'gauge', Dsname: 'value'
    # TYPE collectd_package_1_power_power gauge
    collectd_package_1_power_power{instance="localhost"} 26.5859645878891 XXXXX

    # collectd/write_prometheus 5.11.0.70.gd4c3c59 at localhost
```

### 3: Install Kube State Metrics

Kube State Metrics reads information from a Kubernetes cluster and makes it available to Prometheus. In our use case we're looking for basic information about the pods currently running on the cluster. Kube state metrics can be installed using helm, the Kubernetes package manager, which comes preinstalled in the BMRA.
 
``
helm install "master" stable/kube-state-metrics
``

We can check to see that its running with:

``
kubectl get pods -l app.kubernetes.io/name=kube-state-metrics
``

### 3: Configure Prometheus and the Prometheus adapter
Now that we have our metrics collections agents up and running, Prometheus and Prometheus Adapter, which makes Prometheus metrics available inside the cluster to Telemetry Aware Scheduling, need to be configured to scrape metrics from collectd and Kube State Metrics and to calculate the compound power metrics linking pods to power usage.

``kubectl apply -f $POW_DIR/prometheus/prometheus-config.yaml && kubectl delete pods -nmonitoring -lapp=prometheus -lcomponent=server && kubectl apply -f $POW_DIR/prometheus/custom-metrics-config.yml && kubectl delete pods -n custom-metrics -lapp=prometheus-adapter``

The above command creates a new updated configuration and reboots Prometheus and the Prometheus Adapter. Collectd power metrics should now be available in the Prometheus database and, more importantly, inside the Kubernetes cluster itself. In order to confirm we can run a raw metrics query against the Kubernetes api.

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

### 4: Create TAS Telemetry Policy for power
On the Kubernetes BMRA set up Telemetry Aware Scheduling is already installed and integrated with the Kubernetes control plane. In order to allow our cluster to make scheduling decisions based on current power usage we simply need to create a telemetry policy and associate appropriate pods with that policy.

``kubectl apply -f $POW_DIR/tas-policy.yaml``

We can see our policy by running:

``kubectl describe taspolicy power-sensitive-scheduling-policy``

### 5: Create horizontal autoscaler for power
The Horizontal Pod Autoscaler is an in-built part of Kubernetes which allows scaling decisions to be made based on arbitrary metrics. In this case we're going to scale our workload based on the average power usage on nodes with current instances of our workload. 

To create this Autoscaler:

``kubectl apply -f $POW_DIR/power-autoscaler.yaml``

We can see the details of our autoscaler with:

``kubectl describe hpa power-autoscaler``

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

 kubectl logs -l app.kubernetes.io/name=telemetry-aware-scheduling -c tas-extender 

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

watch kubectl describe hpa power-autoscaler

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

