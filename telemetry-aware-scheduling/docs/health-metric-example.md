# Health Metric Demo

**NOTICE: This is a demo and it is not ready for production usage.**

This document describes the implementation of a health metric telemetry policy using Telemetry Aware Scheduling.
It will show the full process from applying the correct policy to descheduling based on platform metrics.


The files referenced in this example are in the [health-metric-demo directory](../deploy/health-metric-demo)
## A note on the health metric
This worked example uses a synthetic signal called health-metric. 

For a look at a real health metric using Intel Resource Director Technology (RDT) and Reliability, Availability and Serviceability (RAS) metrics the [white paper on Closed Loop Automation and TAS has a detailed description.](https://builders.intel.com/docs/networkbuilders/closed-loop-platform-automation-service-healing-and-platform-resilience.pdf)
A video of that closed loop in action is available [here](https://networkbuilders.intel.com/closed-loop-automation-telemetry-aware-scheduler-for-service-healing-and-platform-resilience-demo) 

## Assumptions 
This guide requires TAS be running as described in the README, and that the [custom metrics pipeline](custom-metrics.md) is supplying it with up to date metrics. Also required is a multinode Kubernetes set-up with user access to all three machines in the cluster.

There should be a text file (with extension .prom) in the /tmp/node-metrics/ folder that contains health metrics in [the Prometheus text format](https://github.com/prometheus/docs/blob/master/content/docs/instrumenting/exposition_formats.md#text-format-details).

If the helm charts were used to install the metrics pipeline this directory will already be created. If another method of setting up Node Exporter was followed the [textfile collector](https://github.com/prometheus/node_exporter#textfile-collector) will need to be enabled in Node Exporter's configuration.


## Setting the health metric
With our health metric the file at /tmp/node-metrics/text.prom should look like:

````node_health_metric 0````

Any change in the value in the file will be read by the prometheus node exporter, and will propagate through the metrics pipeline and made accessible to TAS. The context for which node this information is coming from is provided by the scrape configuration in Prometheus and Prometheus Adapter.

To set the health metric on remote nodes we can use: 

``echo 'node_health_metric ' <METRIC VALUE> | ssh <USER@NODE_NAME> -T "cat > /node-metrics/text.prom"``

A shell script called [set-health](../deploy/health-metric-demo/set-health.sh) is in the [deploy/health-metric-demo](../deploy/health-metric-demo) folder. It takes two arguments, the first being USER@NODENAME and the second a number value to set the health metric. 

To set health metric to 1 on node "myfirstnode" with user blue:
``./set-health blue@myfirstnode 1``

The script assumes a user with write access to /tmp/ and will require a password, or ssh key, to log into the nodes to set the metric. 

If the metric is being picked up properly by the custom metrics API it will return on the command:
```
sudo kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/nodes/*/health_metric" | jq .
```
It may take a couple of minutes for the metric to be initially scraped.

## Setting the workspace

``
kubectl create namespace health-metric-demo
``

## Declare a Telemetry Policy
A Telemetry Policy should be declared using kubectl apply -f <NAME_OF_FILE>. Our [demo health metric policy](../deploy/health-metric-demo/health-policy.yaml) is:

````
apiVersion: telemetry.intel.com/v1alpha1
kind: TASPolicy
metadata:
  name: demo-policy
  namespace: health-metric-demo
spec:
  strategies:
    deschedule:
      rules:
      - metricname: health_metric
        operator: Equals
        target: 2
    dontschedule:
      rules:
      - metricname: health_metric
        operator: Equals
        target: 1
      - metricname: health_metric
        operator: Equals
        target: 2
    scheduleonmetric:
      rules:
      - metricname: health_metric
        operator: LessThan
````
This policy has three strategies included:
- **dontschedule** will refuse to schedule the pod on a node if if that node's health metric is equal to 1 or if it's equal to 2.
- **deschedule** will fire if the health metric on a node is equal to 2. Any pods on that node linked to this policy will become candidates for descheduling.
- **scheduleonmetric** will cause a new pod to be more likely to schedule on a node with a lower health metric.

## Seeing the impact
After the above policy is declared the Telemetry Policy Controller will begin updating metrics associated with it and tracking nodes that are violating the deschedule targets. At this point the controller logs should be printing out these metrics.

### scheduleonmetric
The Scheduler Extender will evaluate metrics for pods ahead of their placement. To see it at work run ``kubectl apply -f demo-pod.yaml`` in the health-metric-demo directory.
The pod spec here is:

```
apiVersion: apps/v1
kind: Deployment
metadata:
  namespace: health-metric-demo
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
        telemetry-policy: demo-policy
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        imagePullPolicy: IfNotPresent
        resources:
          limits:
            telemetry/scheduling: 1
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: scheduling-policy
                    operator: NotIn
                    values:
                      - violating
```
The line ``telemetry-policy: demo-policy`` links each pod created by the above declared deployment policy to the policy declared above. Note that a pod and policy must be in the same namespaces to interact with each other. The deployment also contains a resource limit request for telemetry/scheduling. This is what causes it to be sent to TAS by the current scheduler. The nodeAffinity section is what signals to descheduler to remove the pod from a node. 

On executing this declaration the scheduler extender should produce logs like :
```
2019/08/19 15:09:14 Filtered nodes available for demo-policy : NODE_A NODE_B NODE_C
```
Where NODE* is the name of each node and the numbers are priorities associated with them.

### dontschedule
Using the metrics script we'd like to make some of our nodes unsuitable for deployment. Assuming a three node deployment run 
``./set-health <USER@NODENAME1> 1``
To see the scheduler take this change into account run:
``kubectl scale -f demo-pod.yaml --replicas 2``.
This should produce extender logs like:
```
2019/08/19 15:30:59 NODE_B health_metric = 1
2019/08/19 15:30:59 NODE_A health_metric = 0
2019/08/19 15:30:59 NODE_A violating : health_metric Equals 1
2019/08/19 15:30:59 NODE_C health_metric = 0
2019/08/19 15:30:59 Filtered nodes available for demo-policy : NODE_A NODE_C
```
We can see that only two of our three nodes was deemed suitable because it didn't break the dontschedule rule declared in our Telemetry Scheduling Policy.

### Deschedule
Descheduler can be installed as a binary with the instructions from the [project repo](https://github.com/kubernetes-sigs/descheduler). This demo has been tested with the following Descheduler versions:
1. **v0.23.1** and older
2. **v0.27.1**
3. **v0.28.1**

Telemetry Aware Scheduler and the following Descheduler versions **v0.24.x** to **v0.26.x** seem to have compatibility issues (https://github.com/intel/platform-aware-scheduling/issues/90#issuecomment-1169012485 links to an issue cut to the Descheduler project team). The problem seems to have been fixed in Descheduler **v0.27.1**.

A policy file in the health-metric-demo should be passed to the descheduler as a flag.

Assuming descheduler is on the $PATH the command to run it is:

``descheduler --policy-config-file deploy/health-metric-demo/descheduler-policy.yaml --kubeconfig <PATH_TO_KUBECONFIG> --logtostderr -v 2``


In order to see deschedule in action find out where our demo pods are scheduled by running:
```
kubectl get pods -l app=demo -o wide
```
The output of this command will give the name of each node with one of our demo pods on it. Let's set one of these nodes as unhealthy using the set_health script:

```./set_health <NODE_NAME> 2```

Watching the controller logs should result in something like :

```   
2019/08/19 16:45:54 NODE_C violating demo-policy: health_metric Equals 2
2019/08/19 16:45:54 demo-app-6558d5ffbf-5lw2t labeled with violate=demo-policy
```

Now that the node is labelled, descheduler can be run using:

```./run_descheduler```

```
I0819 17:21:26.627556   30289 label_violation.go:30] Processing node: "NODE_C"
I0819 17:21:26.690462   30289 label_violation.go:41] Evicted pod: "demo-app-6558d5ffbf-fj9xf" (<nil>)
I0819 17:21:26.690876   30289 label_violation.go:30] Processing node: "NODE_B"
I0819 17:21:26.699471   30289 label_violation.go:30] Processing node: "NODE_A"
```
Our pod will then be rescheduled onto a healthier node based on its TAS Policy

## Troubleshooting
This guide assumes a set up in line with  [custom-metrics.md](custom-metrics.md), that is the Metrics API set up using prometheus on a kubeadm cluster.
### Proxy issues
It's important to ensure that there are no proxies in place between the binaries and the host machine. This means ensuring that the master IP and localhost are both in no_proxy environmental variables.
This ensures that the controller and extender aren't rerouted when querying the API server.
### Scheduler Extender produces no logs
If the scheduler extender doesn't produce logs past "Extender listening on..." It is not being contacted by the kubernetes scheduler.
Scheduler logs can be accessed using 
``kubectl logs -l component=kube-scheduler -n kube-system``
If the logs mention issues reaching the scheduler extender recheck the config map with
``kubectl describe configmap -n kube-system scheduler-extender-policy``
Pay specific attention to the IP address (by default localhost and port passed to the scheduler)  
If the scheduler logs don't report any errors contacting the scheduler extender it's not looking at the correct config and the steps to configure scheduler extender in the README should be followed again.

### Health Metric troubleshooting 
```
sudo kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/nodes/*/health_metric" | jq .
```
If the health metric does not appear at the above command there has been some issue with setting the metric.

The metric goes from file ( /tmp/node-metrics/text.prom) to Node Exporter to Prometheus to Metrics API. It is possible to follow it at each step.
- **file**: Output from ``cat /tmp/node-metrics/text.prom`` should look like:  ``node_health_metric 1``
- **Node Exporter**: Assuming Node Exporter is exposing metrics at port 9100. Output from ``curl localhost:9100/metrics | grep health_metric`` should produce:  
```
HELP node_health_metric Metric read from /host/node-metrics/text.prom 
# TYPE node_health_metric untyped   
node_health_metric 1
```
- **Prometheus** : Assuming Prometheus is exposing its dashboard at localhost:30000/metrics node_health_metric should be available in the search bar for each node on which it's set.

If the metric is appearing in prometheus but it is not appearing in the custom metrics API call check if other node metrics are appearing in the custom metrics API. For example:
```
sudo kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/nodes/*/load1" | jq .
```
Or, more generally:

```
sudo kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/" | jq . | grep node
```

If this command doesn't return multiple metrics with paths like **/nodes/*/<METRIC_NAME> it's likely there is some issue with the scrape configuration for the custom metrics api. More info can be seen at [custom-metrics.md](custom-metrics.md)

### Descheduler

If the pods are not moving to the non-violating node when running the descheduler comment, please check the descheduler logs for any errors.

#### Insufficient telemetry/scheduling

Example error:
````
I0629 17:54:44.063789       1 node.go:168] "Pod does not fit on node" pod="labeling-demo/demo-app-label-6b9cc98bf4-rd9mx" node="NODE-A"
I0629 17:54:44.063807       1 node.go:170] "insufficient telemetry/scheduling"
````
To address this error we need to patch all nodes in the cluster by adding the missing resource. To do so:

1. On the control plane node, in a new window run:

````
kubectl proxy
````
2. On the control plane node, for each node run:

````
curl --header "Content-Type: application/json-patch+json" --request PATCH --data '[{"op": "add", "path": "/status/capacity/telemetry~1scheduling", "value": "10"}]' http://localhost:8001/api/v1/nodes/$NODE_NAME/status
````

**[NOTE]** The value chosen for this example is random. The only constraints are: the value has to be a positive integer (> 0) and high enough to be able to fulfill the resource specs. More details [here](https://github.com/intel/platform-aware-scheduling/tree/master/telemetry-aware-scheduling#linking-a-workload-to-a-policy)
