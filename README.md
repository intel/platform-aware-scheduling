# Telemetry Aware Scheduling
Telemetry Aware Scheduling (TAS) makes telemetry data available to scheduling and descheduling decisions in Kubernetes. Through a user defined policy, TAS enables rule based decisions on pod placement powered by up to date platform metrics. Policies can be applied on a workload by workload basis - allowing the right indicators to be used to place the right pod.

For example - a pod that requires certain cache characteristics can be schedule on output from Intel® RDT metrics. Likewise a combination of RDT, RAS and other platform metrics can be used to provide a signal for the overall health of a node and be used to proactively ensure workload resiliency.


**This software is a pre-production alpha version and should not be deployed to production servers.**


## Components
Telemetry Aware Scheduling is made up of two components deployed in a single pod on a Kubernetes Cluster. 

### Telemetry Aware Scheduler Extender
Telemetry Aware Scheduler Extender is contacted by the generic Kubernetes Scheduler every time it needs to make a scheduling decision.
The extender checks if there is a telemetry policy associated with the workload. 
If so, it inspects the strategies associated with the policy and returns opinions on pod placement to the generic scheduler.
The scheduler extender has two strategies it acts on -  scheduleonmetric and dontschedule.
This is implemented and configured as a [Kubernetes Scheduler Extender.](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/#cluster-level-extended-resources)

### Telemetry Policy Controller
The Telemetry Policy Controller consumes TAS Policies - a Custom Resource. The controller parses this policy for deschedule, scheduleonmetric and dontschedule strategies and places them in a cache to make them locally available to all TAS components.
It consumes new Telemetry Policies as they are created, removes them when deleted, and updates them as they are changed.
The policy controller also monitors the current state of policies to see if they are violated. For example if it notes that a deschedule policy is violated it labels the node as a violator allowing pods relating to that policy to be descheduled.

## Usage
A worked example for TAS is available [here](docs/health-metric-example.md)
### Strategies
There are three strategies that TAS acts on. 
 
 **1 scheduleonmetric** has only one rule. It is consumed by the Telemetry Aware Scheduling Extender and prioritizes nodes based on a comparator and an up to date metric value.
  - example: **scheduleonmetric** when **cache_hit_ratio** is **GreaterThan**
  
 **2 dontschedule** strategy has multiple rules, each with a metric name and operator and a target. A pod with this policy will never be scheduled on a node breaking any one of these rules.
 - example: **dontschedule** if **gpu_usage** is **GreaterThan 10**
 
 **3 deschedule** is consumed by the Telemetry Policy Controller. If a pod with this policy is running on a node that violates it can be descheduled with the kubernetes descheduler.
- example: **deschedule** if **network_bandwidth_percent_free** is **LessThan 10**

The policy definition section below describes how to actually create these strategies in a kubernetes cluster.

### Quick set up
The deploy folder has all of the yaml files necessary to get Telemetry Aware Scheduling running in a Kubernetes cluster. Some additional steps are required to configure the generic scheduler and metrics endpoints.

#### Custom Metrics Pipeline
TAS relies on metrics from the custom metrics pipeline. A guide on setting up the custom metrics pipeline to have it operate with TAS is [here.](./docs/custom-metrics.md)
If this pipeline isn't set up, and node level metrics aren't exposed through it, TAS will have no metrics on which to make decisions.

#### Extender configuration
Note: a shell script that shows these steps can be found [here](deploy/extender-configuration). This script should be seen as a guide only, and will not work on most Kubernetes installations.

The extender configuration files can be found under deploy/extender-configuration.
TAS Scheduler Extender needs to be registered with the Kubernetes Scheduler. In order to do this a configmap should be created like the below:
````
apiVersion: v1alpha1
kind: ConfigMap
metadata:
  name: scheduler-policy
  namespace: kube-system
data:
  policy.cfg: |
    {
        "kind" : "Policy",
        "apiVersion" : "v1",
        "extenders" : [
            {
              "urlPrefix": "https://tas-service.default.svc.cluster.local:9001",
              "apiVersion": "v1",
              "prioritizeVerb": "scheduler/prioritize",
              "filterVerb": "scheduler/filter",
              "weight": 1,
              "enableHttps": true,
              "managedResources": [
                   {
                     "name": "telemetry/scheduling",
                     "ignoredByScheduler": true
                   }
              ],
              "ignorable": true
          }
         ]
    }

````
This file can be found [in the deploy folder](./deploy/extender-configuration/scheduler-extender-configmap.yaml). This configmap can be created with ``kubectl apply -f ./deploy/scheduler-extender-configmap.yaml``
The scheduler requires flags passed to it in order to know the location of this config map. The flags are:
````
    - --policy-configmap=scheduler-extender-policy
    - --policy-configmap-namespace=kube-system
````

If scheduler is running as a service these can be added as flags to the binary. If scheduler is running as a container - as in kubeadm - these args can be passed in the deployment file.
Note: For Kubeadm set ups some additional steps may be needed.
1) Add the ability to get configmaps to the kubeadm scheduler config map. (A cluster role binding for this is at deploy/extender-configuration/configmap-getter.yaml)
2) Add the ``dnsPolicy: ClusterFirstWithHostNet`` in order to access the scheduler extender by service name.

After these steps the scheduler extender should be registered with the Kubernetes Scheduler.

#### Deploy TAS
Telemetry Aware Scheduling uses go modules. It requires Go 1.13+ with modules enabled in order to build. TAS has been tested with Kubernetes 1.14+. TAS was tested on Intel® Server Board S2600WF-Based Systems (Wolf Pass).
A yaml file for TAS is contained in the deploy folder along with its service and RBAC roles and permissions.

**Note:** If run without the unsafe flag a secret called extender-secret will need to be created with the cert and key for the TLS endpoint.
TAS will not deploy if there is no secret available with the given deployment file.

A secret can be created with:

``
kubectl create secret tls extender-secret --cert /etc/kubernetes/<PATH_TO_CERT> --key /etc/kubernetes/<PATH_TO_KEY> 
``
In order to build and deploy run:

``make build && make image && kubectl apply -f deploy/``

After this is run TAS should be operable in the cluster and should be visible after running ``kubectl get pods``

#### Descheduling workloads
Where there is a descheduling strategy in a policy, TAS will label nodes as violators if they break any of the associated rules. In order to deschedule these workloads the [Kubernetes Descheduler](https://github.com/kubernetes-sigs/descheduler) should be used.
The strategy file for Descheduler should be:
````
apiVersion: "descheduler/v1alpha1"
kind: "DeschedulerPolicy"
strategies:
  "RemovePodsViolatingNodeAffinity":
    enabled: true
    params:
      nodeAffinityType:
      - "requiredDuringSchedulingIgnoredDuringExecution"
````
This file is available [here](deploy/health-metric-demo/descheduler-policy.yaml)

### Policy definition {: #policy-definition}
A Telemetry Policy can be created in Kubernetes using ``kubectl apply -f`` on a valid policy file. 
The structure of a policy file is : 

````
apiVersion: telemetry.intel.com/v1alpha1
kind: TASPolicy
metadata:
  name: scheduling-policy
  namespace: default
spec:
  strategies:
    deschedule:
      rules:
      - metricname: node_metric
        operator: Equals
        target: -1
    dontschedule:
      rules:
      - metricname: node_metric
        operator: LessThan
        target: 10
    scheduleonmetric:
      rules:
      - metricname: node_metric
        operator: GreaterThan
````
There are three strategy types in a policy file and rules associated with each.
 - **scheduleonmetric** has only one rule. It is consumed by the Telemetry Aware Scheduling Extender and prioritizes nodes based on the rule.
 - **dontschedule** strategy has multiple rules, each with a metric name and operator and a target. A pod with this policy will never be scheduled on a node breaking any one of these rules.
 - **deschedule** is consumed by the Telemetry Policy Controller. If a pod with this policy is running on a node that violates that pod can be descheduled with the kubernetes descheduler.

dontschedule and deschedule - which incorporate multiple rules - function with an OR operator. That is if any single rule is broken the strategy is considered violated.
Telemetry policies are namespaced, meaning that under normal circumstances a workload can only be associated with a pod in the same namespaces.

### Configuration flags
The below flags can be passed to the binaries at run time.

#### TAS Policy Controller
name |type | description| usage | default|
-----|------|-----|-------|-----|
|kubeConfig| string |location of kubernetes configuration file | -kubeConfig /root/filename|~/.kube/config
|syncPeriod|duration string| interval between refresh of telemetry data|-syncPeriod 1m| 1s
|cachePort | string | port number at which the cache server will listen for requests | --cachePort 9999 | 8111

#### TAS Scheduler Extender
name |type | description| usage | default|
-----|------|-----|-------|-----|
|syncPeriod|duration string| interval between refresh of telemetry data|-syncPeriod 1m| 1s
|port| int | port number on which the scheduler extender will listen| -port 32000 | 9001
|cert| string | location of the cert file for the TLS endpoint | --cert=/root/cert.txt| /etc/kubernetes/pki/ca.key
|key| string | location of the key file for the TLS endpoint| --key=/root/key.txt | /etc/kubernetes/pki/ca.key
|unsafe| bool | whether or not to listen on a TLS endpoint with the scheduler extender | --unsafe=true| false

## Linking a workload to a policy 
Pods can be linked with policies by adding a label of the form ``telemetry-policy=<POLICY-NAME>``
This also needs to be done inside higher level workload types i.e. deployments.

For example,  in a deployment file: 
```
apiVersion: extensions/v1beta1
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
        telemetry-policy: scheduling-policy
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

Here the policy scheduling-policy will apply to all pods created by this deployment.
There are three changes to the demo policy here:
- A label ``telemetry-policy=<POLICYNAME>`` under the pod template which is used by the scheduler to identify the policy.
- A resources/limits entry requesting the resource telemetry/scheduling. This is used to restrict the use of TAS to only selected pods. If this is not in a pod spec the pod will not be scheduled by TAS.
- Affinity rules which add a requiredDuringSchedulingIgnoredDuringExecution affinity to nodes which are labelled ``<POLICYNAME>=violating`` This is used by the descheduler to identify pods on nodes which break their TAS telemetry policies.

### Security
TAS Policy Controller is set up to use in-Cluster config in order to access the Kubernetes API Server. When deployed inside the cluster this along with RBAC controls configured in the installation guide, will give it access to the required resources.
If outside the cluster TAS Policy Controller will try to use a kubernetes config file in order to get permission to get resources from the API server. This can be passed with the --kubeconfig flag to the controller.

TAS Scheduler Extender contacts api server in the same way as policy controller. An identical flag  --kubeConfig can be passed if it's operating outside the cluster.
Additionally TAS Scheduler Extender listens on a TLS endpoint which requires a cert and a key to be supplied.
These are passed to the executable using command line flags. In the provided deployment these certs are added in a Kubernetes secret which is mounted in the pod and passed as flags to the executable from there.

## Communication and contribution

Report a bug by [filing a new issue](https://github.com/intel/telemetry-aware-scheduling/issues).

Contribute by [opening a pull request](https://github.com/intel/telemetry-aware-scheduling/pulls).

Learn [about pull requests](https://help.github.com/articles/using-pull-requests/).

**Reporting a Potential Security Vulnerability:** If you have discovered potential security vulnerability in TAS, please send an e-mail to secure@intel.com. For issues related to Intel Products, please visit [Intel Security Center](https://security-center.intel.com).

It is important to include the following details:

- The projects and versions affected
- Detailed description of the vulnerability
- Information on known exploits

Vulnerability information is extremely sensitive. Please encrypt all security vulnerability reports using our [PGP key](https://www.intel.com/content/www/us/en/security-center/pgp-public-key.html).

A member of the Intel Product Security Team will review your e-mail and contact you to collaborate on resolving the issue. For more information on how Intel works to resolve security issues, see: [vulnerability handling guidelines](https://www.intel.com/content/www/us/en/security-center/vulnerability-handling-guidelines.html).

