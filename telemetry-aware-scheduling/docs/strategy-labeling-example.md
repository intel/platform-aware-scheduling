# Labeling Strategy Example
This guide shows how to implement a labeling strategy in the TAS policy and having an application that runs within that policy.
In the [Health metric demo](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/docs/health-metric-example.md#setting-the-health-metric), it is shown how the *deschedule* strategy works when policy strategy rules are violated i.e., it marks the node with a pre-defined label "violating". This allows the k8s Descheduler to evict all the pod in that node. The *deschedule* strategy, however, is not flexible in relation to labels that can be used to mark the node.
The addition of *labeling* into the available strategies gives the desired flexibility to go beyond a single fixed key:value pair such as "policyName: violating".  The *labeling* strategy gives extra support for pods/workloads when specific physical devices/resources per node is required. This is then achieved, by linking the policy rules and the labeling capacity to the specific node resources and the evaluation of their metrics values.
In this demo, a simple case is exemplified when a node is labeled by a customized label as the policy rule is broken. Also, we verify that running Pods, in the nodes whose metrics are no longer obeying the policy rules, are evicted by the K8s Descheduler.
This guide requires at least two worker nodes in a Kubernetes cluster set-up with user permission level, and it needs the [TAS](https://github.com/intel/platform-aware-scheduling/tree/master/telemetry-aware-scheduling#deploy-tas) and the [custom metrics](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/docs/custom-metrics.md#quick-install) pipeline to be running.

### Setting the metrics
The metrics that will be scraped by node-exporter can be set in each of the files. Please add the metrics name and values in /tmp/node-metrics/metrics.prom per worker node.

In node1:
````node_metric_card0 200````

In node2:
````node_metric_card1 90````

The values can be changed via a shell script like one applied to the [health metric demo](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/docs/health-metric-example.md#setting-the-health-metric). Note that the script assumes a user level with write access to /tmp/ and will require a password, or ssh key, to log into the nodes to set the metric.
Any change in the value in the respective file for each node will be read by the Prometheus Node Exporter, and will propagate through the metrics pipeline to be accessible by TAS.
If the metric is being picked up properly by the custom metrics API it will return on the command for metric_card1:

````kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/nodes/*/metric_card1" | jq .````

Note, it may take some time for the metric to be initially scraped.


### Deploy a Telemetry Policy

Create the appropriate namespace with: `kubectl create ns labeling-demo`

````
cat <<EOF | kubectl create -f  -
apiVersion: telemetry.intel.com/v1alpha1
kind: TASPolicy
metadata:
  name: labeling-policy
  namespace: labeling-demo
spec:
  strategies:
    labeling:
      rules:
      - metricname: metric_card0
        operator: GreaterThan
        target: 100
        labels: ["card0=true"]
      - metricname: metric_card1
        operator: GreaterThan
        target: 200
        labels: ["card1=true"]
EOF
````

The Telemetry Policy can be verified by using:

````kubectl get taspolicy/labeling-policy -n labeling-demo````

Once the above policy is deployed TAS will begin to update metrics associated with it and label the nodes that are violating any of the strategy rules. The TAS logs may display:

````
I1125 12:32:01.187307       1 enforce.go:241] "Evaluating labeling-policy" component="controller"
I1125 12:32:01.187394       1 strategy.go:58] "node1 violating labeling-policy: metric_card0 GreaterThan 100 [card0=true]" component="controller"
I1125 12:32:01.187412       1 strategy.go:74] Violated rules: metric_card0 GreaterThan 100 [card0=true]
I1125 12:32:01.187490       1 enforce.go:174] "Node node1 violating labeling-policy," component="controller"
````

The node1 should now have a label corresponding to the broken rule. To verify this, run

````
kubectl get node node1 -o json | jq -r '[{label: .metadata.labels}]'
````

The output should display:

````
[
  {
    "label": {
      "beta.kubernetes.io/arch": "amd64",
      "beta.kubernetes.io/os": "linux",
      "kubernetes.io/arch": "amd64",
      "kubernetes.io/hostname": "node1",
      "kubernetes.io/os": "linux",
      "node-role.kubernetes.io/control-plane": "",
      "node-role.kubernetes.io/master": "",
      "node.kubernetes.io/exclude-from-external-load-balancers": "",
      "telemetry.aware.scheduling.labeling-policy/card0": "true"
    }
  }
]
````

By changing the metric node_metric_card0 value from 200 to 100, for instance, the label will be removed, and any previous reference to the violated rules disappears from the logs. Note that it can take some time for the metric values to reach TAS. Similar behavior is observed if the metric node_metric_card1 is changed to a value greater than the target value in the policy. Therefore, each node (with metrics that are being pulled via custom-metrics API to TAS) will be labeled according to the linked policy.

### Seeing the impact on application deployment

First, reset the metric values by using the metrics for each node as:

In node1: 
````node_metric_card0 200````

In node2: 
````node_metric_card1 90````


TAS evaluates metrics in the nodes ahead of pods deployments that use the TAS policy. Then, the application for the pods can be deployed in the cluster by:

````
cat <<EOF | kubectl create -f  -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-app-label
  namespace: labeling-demo
  labels:
    app: demo-label
spec:
  replicas: 1
  selector:
    matchLabels:
      app: demo-label
  template:
    metadata:
      labels:
        app: demo-label
        telemetry-policy: labeling-policy
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
                  - key:  telemetry.aware.scheduling.labeling-policy/card0
                    operator: NotIn
                    values:
                      - "true"
                  - key:  telemetry.aware.scheduling.labeling-policy/card1
                    operator: NotIn
                    values:
                      - "true"
EOF
````

If any of the nodes is labeled with the telemetry.aware.scheduling.labeling-policy/card0 or telemetry.aware.scheduling.labeling-policy/card1 with the respective value "true", then the corresponding node is NOT available to receive workloads linked to labeling-policy.

Verify by deploying the workload:

````
kubectl get deploy/demo-app-label -n labeling-demo
````

Verify the Pod from the deployment:

````
kubectl get po -l app=demo-label -n labeling-demo
````

The pod will avoid being scheduled to node1 and will be scheduled on node2. This effect can be enhanced by scaling up:

````
kubectl scale deploy demo-app-label -n labeling-demo --replicas=5
````

All the pods will be scheduled and deployed to node2, and this can be verified by:

````
kubectl get po -l app=demo-label -o wide -n labeling-demo| awk {'print $1" "$2" "$3" "$5" "$7'} | column -t

NAME                             READY  STATUS   AGE  NODE
demo-app-label-68b4b587f9-8sxv7  1/1    Running  12s  node2
demo-app-label-68b4b587f9-gjh2n  1/1    Running  12s  node2
demo-app-label-68b4b587f9-hw88m  1/1    Running  12s  node2
demo-app-label-68b4b587f9-w69hb  1/1    Running  23s  node2
demo-app-label-68b4b587f9-xjll4  1/1    Running  12s  node2
````

To invert the situation, the metric values need to be changed to:

In node1: 
````node_metric_card0 90````

In node2: 
````node_metric_card1 900````

Once the new values are detected by TAS, the policy label in node1 is removed and node2 is now labeled:

````kubectl get node node2 -o json | jq -r '[{label: .metadata.labels}]'````

The output should display:

````
[
  {
    "label": {
      "beta.kubernetes.io/arch": "amd64",
      "beta.kubernetes.io/os": "linux",
      "kubernetes.io/arch": "amd64",
      "kubernetes.io/hostname": "node2",
      "kubernetes.io/os": "linux",
      "telemetry.aware.scheduling.labeling-policy/card1": "true"
    }
  }
]
````

When the deployment is now scaled up to 10 by:

````
kubectl scale deploy demo-app-label --replicas=10 -n labeling-demo
````

All the new Pods are scheduled only on node1. Check this by:

````
kubectl get po -l app=demo-label -o wide -n labeling-demo --sort-by=.spec.nodeName | awk {'print $1" "$2" "$3" "$5" "$7'} | column -t
NAME                             READY  STATUS   AGE    NODE
demo-app-label-68b4b587f9-2g96q  1/1    Running  42s    node1
demo-app-label-68b4b587f9-sdzmc  1/1    Running  42s    node1
demo-app-label-68b4b587f9-8xgjd  1/1    Running  42s    node1
demo-app-label-68b4b587f9-99bwb  1/1    Running  42s    node1
demo-app-label-68b4b587f9-gftzs  1/1    Running  42s    node1
demo-app-label-68b4b587f9-gjh2n  1/1    Running  3m40s  node2
demo-app-label-68b4b587f9-hw88m  1/1    Running  3m40s  node2
demo-app-label-68b4b587f9-8sxv7  1/1    Running  3m40s  node2
demo-app-label-68b4b587f9-w69hb  1/1    Running  3m51s  node2
demo-app-label-68b4b587f9-xjll4  1/1    Running  3m40s  node2
````

When returning to the previous values by changing the metric value of card0 in node1:

In node1: 
````node_metric_card0 200````

Both nodes have labels card0 for node1 and card1 for node2, which means the workload will be in a *pending* state when the deployment is scaled to 15

````
NAME                             READY  STATUS   AGE    NODE
demo-app-label-68b4b587f9-6jp7x  0/1    Pending  12s    <none>
demo-app-label-68b4b587f9-jlt2v  0/1    Pending  12s    <none>
demo-app-label-68b4b587f9-mtcjh  0/1    Pending  12s    <none>
demo-app-label-68b4b587f9-629qf  0/1    Pending  12s    <none>
demo-app-label-68b4b587f9-bcd5l  0/1    Pending  12s    <none>
demo-app-label-68b4b587f9-sdzmc  1/1    Running  2m15s  node1
demo-app-label-68b4b587f9-8xgjd  1/1    Running  2m15s  node1
demo-app-label-68b4b587f9-99bwb  1/1    Running  2m15s  node1
demo-app-label-68b4b587f9-gftzs  1/1    Running  2m15s  node1
demo-app-label-68b4b587f9-2g96q  1/1    Running  2m15s  node1
demo-app-label-68b4b587f9-hw88m  1/1    Running  5m13s  node2
demo-app-label-68b4b587f9-gjh2n  1/1    Running  5m13s  node2
demo-app-label-68b4b587f9-8sxv7  1/1    Running  5m13s  node2
demo-app-label-68b4b587f9-w69hb  1/1    Running  5m24s  node2
demo-app-label-68b4b587f9-xjll4  1/1    Running  5m13s  node2
````

Once the metric changes for a given node, and it returns to a schedulable condition, i.e., non-violating the labeling rules, then the workloads will be scheduled to run at the node referred.


### Descheduler#
[Kubernetes Descheduler](https://github.com/kubernetes-sigs/descheduler) allows control of pod evictions in the cluster after being bound to a node. Descheduler, based on its policy, finds pods that can be moved and evicted.  There are many ways to install and run the K8s [Descheduler](https://github.com/kubernetes-sigs/descheduler#quick-start). Here, we have executed it as a [deployment](https://github.com/kubernetes-sigs/descheduler#run-as-a-deployment) by using the latest available descheduler version.

This demo has been tested with the following Descheduler versions:
1. **v0.23.1** and older
2. **v0.27.1**
3. **v0.28.0**

Telemetry Aware Scheduler and the following descheduler versions **v0.24.x** to **v0.26.x** seem to have compatibility issues (https://github.com/intel/platform-aware-scheduling/issues/90#issuecomment-1169012485 links to an issue cut to the Descheduler project team). The problem seems to have been fixed in Descheduler **v0.27.1**.

In a shell terminal, deploy the Descheduler files:

````
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/descheduler/master/kubernetes/base/rbac.yaml
````
````
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/descheduler/master/kubernetes/deployment/deployment.yaml
````
````
cat <<EOF | kubectl create -f  -
apiVersion: v1
kind: ConfigMap
metadata:
  name: descheduler-policy-configmap
  namespace: kube-system
data:
  policy.yaml: |
    apiVersion: "descheduler/v1alpha1"
    kind: "DeschedulerPolicy"
    strategies:
      "RemovePodsViolatingNodeAffinity":
        enabled: true
        params:
          nodeAffinityType:
            - "requiredDuringSchedulingIgnoredDuringExecution"
EOF
````

The Descheduler pod can be verified by:

````
kubectl get po -n kube-system -l app=descheduler

NAME                           READY   STATUS    RESTARTS   AGE
descheduler-5d779f94f9-7ndwm   1/1     Running   0          59m
````

Descheduler logs can be observed by:

````
kubectl logs -l app=descheduler -n kube-system

I1129 13:49:57.044694       1 node.go:46] "Node lister returned empty list, now fetch directly"
I1129 13:49:57.052893       1 node_affinity.go:75] "Executing for nodeAffinityType" nodeAffinity="requiredDuringSchedulingIgnoredDuringExecution"
I1129 13:49:57.052922       1 node_affinity.go:80] "Processing node" node="node1"
I1129 13:49:57.061758       1 node.go:169] "Pod fits on node" pod="default/telemetry-aware-scheduling-5c6f764f96-c65v2" node="node1"
I1129 13:49:57.061825       1 node_affinity.go:80] "Processing node" node="node2"
I1129 13:49:57.074441       1 node.go:169] "Pod fits on node" pod="monitoring/prometheus-operator-764cb46c94-5h2ht" node="node2"
I1129 13:49:57.074483       1 descheduler.go:152] "Number of evicted pods" totalEvicted=0
````

In a second shell terminal, ensure the values for the metrics in each node (/tmp/node-metrics/metric.prom) are set as:

node1: 
````node_metric_card0 200````
node2: 
````node_metric_card1 90````

If the guide has continued immediately after the previous section, then scale down the number of pods to 0 and then scale it up to 3. Otherwise, execute the previous deployments for the policy and the demo-labelling app. Then, scale up to 3 pods.

````
kubectl scale deploy demo-app-label --replicas=3 -n labeling-demo
````

All the pods will be scheduled and deployed to node2. You can verify by:

````
kubectl get po -l app=demo-label -n labeling-demo -o wide | awk {'print $1" "$2" "$3" "$5" "$7'} | column -t
NAME                             READY  STATUS   AGE  NODE
demo-app-label-68b4b587f9-654kp  1/1    Running  97s  node2
demo-app-label-68b4b587f9-6q2s5  1/1    Running  97s  node2
demo-app-label-68b4b587f9-l6dmm  1/1    Running  97s  node2
````

In the Descheduler logs shell terminal you may observe a similar output to:

````
I1129 13:50:57.100321       1 node.go:46] "Node lister returned empty list, now fetch directly"
I1129 13:50:57.110201       1 node_affinity.go:75] "Executing for nodeAffinityType" nodeAffinity="requiredDuringSchedulingIgnoredDuringExecution"
I1129 13:50:57.110235       1 node_affinity.go:80] "Processing node" node="node1"
I1129 13:50:57.121532       1 node.go:169] "Pod fits on node" pod="default/demo-app-gpu-86b9d8cf4f-rnpmk" node="node1"
I1129 13:50:57.121601       1 node.go:169] "Pod fits on node" pod="default/telemetry-aware-scheduling-5c6f764f96-c65v2" node="node1"
I1129 13:50:57.121670       1 node_affinity.go:80] "Processing node" node="node2"
I1129 13:50:57.131202       1 node.go:169] "Pod fits on node" pod="default/demo-app-label-68b4b587f9-8pr48" node="node2"
I1129 13:50:57.131260       1 node.go:169] "Pod fits on node" pod="default/demo-app-label-68b4b587f9-k5g9h" node="node2"
I1129 13:50:57.131293       1 node.go:169] "Pod fits on node" pod="default/demo-app-label-68b4b587f9-lfxl2" node="node2"
I1129 13:50:57.131340       1 node.go:169] "Pod fits on node" pod="monitoring/prometheus-operator-764cb46c94-5h2ht" node="node2"
I1129 13:50:57.131379       1 descheduler.go:152] "Number of evicted pods" totalEvicted=0
````

Descheduler scans nodes and pods according to the descheduler policy in the configMap within a 5 minute period. This descheduling interval can be changed by modifying the corresponding value in the [deployment](https://github.com/kubernetes-sigs/descheduler/blob/master/kubernetes/deployment/deployment.yaml#L29-L30) file. A quick option is editing the value by:
````
kubectl edit deploy descheduler -n kube-system
````

Change the metric values to

node1: 
````node_metric_card0 20````
node2: 
````node_metric_card1 900````

Within such values, the violation of the established policy rule for labeling strategy is now inverted, that is, node1 can be scheduled to receive pods and node2 cannot be scheduled to receive pods and it can have its pods evicted. Logs in the Descheduler in the other terminal may display a similar output to:

````
I1129 13:52:27.184775       1 node.go:46] "Node lister returned empty list, now fetch directly"
I1129 13:52:27.190359       1 node_affinity.go:75] "Executing for nodeAffinityType" nodeAffinity="requiredDuringSchedulingIgnoredDuringExecution"
I1129 13:52:27.190399       1 node_affinity.go:80] "Processing node" node="node1"
I1129 13:52:27.200805       1 node.go:169] "Pod fits on node" pod="default/telemetry-aware-scheduling-5c6f764f96-c65v2" node="node1"
I1129 13:52:27.200926       1 node_affinity.go:80] "Processing node" node="node2"
I1129 13:52:27.210630       1 node.go:165] "Pod does not fit on node" pod="default/demo-app-label-68b4b587f9-8pr48" node="node2"
I1129 13:52:27.210685       1 node.go:147] "Pod can possibly be scheduled on a different node" pod="default/demo-app-label-68b4b587f9-8pr48" node="node1"
I1129 13:52:27.210730       1 node.go:165] "Pod does not fit on node" pod="default/demo-app-label-68b4b587f9-k5g9h" node="node2"
I1129 13:52:27.210783       1 node.go:147] "Pod can possibly be scheduled on a different node" pod="default/demo-app-label-68b4b587f9-k5g9h" node="node1"
I1129 13:52:27.210833       1 node.go:165] "Pod does not fit on node" pod="default/demo-app-label-68b4b587f9-lfxl2" node="node2"
I1129 13:52:27.210875       1 node.go:147] "Pod can possibly be scheduled on a different node" pod="default/demo-app-label-68b4b587f9-lfxl2" node="node1"
I1129 13:52:27.210945       1 node.go:169] "Pod fits on node" pod="monitoring/prometheus-operator-764cb46c94-5h2ht" node="node2"
I1129 13:52:27.210981       1 node_affinity.go:101] "Evicting pod" pod="default/demo-app-label-68b4b587f9-8pr48"
I1129 13:52:27.218527       1 evictions.go:130] "Evicted pod" pod="default/demo-app-label-68b4b587f9-8pr48" reason="NodeAffinity"
I1129 13:52:27.218693       1 node_affinity.go:101] "Evicting pod" pod="default/demo-app-label-68b4b587f9-k5g9h"
I1129 13:52:27.218926       1 event.go:291] "Event occurred" object="default/demo-app-label-68b4b587f9-8pr48" kind="Pod" apiVersion="v1" type="Normal" reason="Descheduled" message="pod evicted by sigs.k8s.io/deschedulerNodeAffinity"
I1129 13:52:27.234149       1 evictions.go:130] "Evicted pod" pod="default/demo-app-label-68b4b587f9-k5g9h" reason="NodeAffinity"
I1129 13:52:27.234274       1 node_affinity.go:101] "Evicting pod" pod="default/demo-app-label-68b4b587f9-lfxl2"
I1129 13:52:27.234831       1 event.go:291] "Event occurred" object="default/demo-app-label-68b4b587f9-k5g9h" kind="Pod" apiVersion="v1" type="Normal" reason="Descheduled" message="pod evicted by sigs.k8s.io/deschedulerNodeAffinity"
I1129 13:52:27.286278       1 evictions.go:130] "Evicted pod" pod="default/demo-app-label-68b4b587f9-lfxl2" reason="NodeAffinity"
I1129 13:52:27.286428       1 descheduler.go:152] "Number of evicted pods" totalEvicted=3
I1129 13:52:27.286503       1 event.go:291] "Event occurred" object="default/demo-app-label-68b4b587f9-lfxl2" kind="Pod" apiVersion="v1" type="Normal" reason="Descheduled" message="pod evicted by sigs.k8s.io/deschedulerNodeAffinity"
````

The pods are then located at node1:

````
kubectl get po -l app=demo-label -n labeling-demo -o wide | awk {'print $1" "$2" "$3" "$5" "$7'} | column -t
NAME                             READY  STATUS   AGE  NODE
demo-app-label-68b4b587f9-5wrdt  1/1    Running  24s  node1
demo-app-label-68b4b587f9-n4lgn  1/1    Running  24s  node1
demo-app-label-68b4b587f9-wnpp7  1/1    Running  24s  node1
````

#### Troubleshooting the descheduler

If the pods are not moved to the non-violating node in the next descheduler cycle, please check the descheduler logs. Make sure the log level is set to **4** or higher.  This can be changed by modifying the corresponding value in the [deployment](https://github.com/kubernetes-sigs/descheduler/blob/master/kubernetes/deployment/deployment.yaml#L31-L32) file. A quick option is editing the value by:

````
kubectl edit deploy descheduler -n kube-system
````

##### Insufficient telemetry/scheduling

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

**[NOTE]** The value chosen for this example is random. The only constraints are: the value has to be a positive integer (> 0) and high enough to be able to fulfill the all the resource specs that want this resource. More details [here](https://github.com/intel/platform-aware-scheduling/tree/master/telemetry-aware-scheduling#linking-a-workload-to-a-policy)


### For clean-up:
````
kubectl delete deploy/demo-app-label taspolicy/labeling-policy -n labeling-demo
kubectl delete ns labeling-demo
kubectl delete cm descheduler-policy-configmap -n kube-system
kubectl delete -f https://raw.githubusercontent.com/kubernetes-sigs/descheduler/master/kubernetes/base/rbac.yaml
kubectl delete -f https://raw.githubusercontent.com/kubernetes-sigs/descheduler/master/kubernetes/deployment/deployment.yaml
````
