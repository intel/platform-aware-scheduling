# PROJECT NOT UNDER ACTIVE MANAGEMENT
This project will no longer be maintained by Intel.  
Intel has ceased development and contributions including, but not limited to, maintenance, bug fixes, new releases, or updates, to this project.  
Intel no longer accepts patches to this project.  
If you have an ongoing need to use this project, are interested in independently developing it, or would like to maintain patches for the open source software community, please create your own fork of this project.  

# Telemetry Aware Scheduling
Telemetry Aware Scheduling (TAS) makes telemetry data available to scheduling and descheduling decisions in Kubernetes. Through a user defined policy, TAS enables rule based decisions on pod placement powered by up to date platform metrics. Policies can be applied on a workload by workload basis - allowing the right indicators to be used to place the right pod.

For example - a pod that requires certain cache characteristics can be schedule on output from Intel® RDT metrics. Likewise a combination of RDT, RAS and other platform metrics can be used to provide a signal for the overall health of a node and be used to proactively ensure workload resiliency.



## Introduction

Telemetry Aware Scheduler Extender is contacted by the generic Kubernetes Scheduler every time it needs to make a scheduling decision.
The extender checks if there is a telemetry policy associated with the workload. 
If so, it inspects the strategies associated with the policy and returns opinions on pod placement to the generic scheduler.
The scheduler extender has two strategies it acts on -  scheduleonmetric and dontschedule.
This is implemented and configured as a [Kubernetes Scheduler Extender.](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/#cluster-level-extended-resources)

The Scheduler consumes TAS Policies - a Custom Resource. The extender parses this policy for deschedule, scheduleonmetric and dontschedule strategies and places them in a cache to make them locally available to all TAS components.
It consumes new Telemetry Policies as they are created, removes them when deleted, and updates them as they are changed.
The extender also monitors the current state of policies to see if they are violated. For example if it notes that a deschedule policy is violated it labels the node as a violator allowing pods relating to that policy to be descheduled.

## Usage
A worked example for TAS is available [here](docs/health-metric-example.md)
### Strategies
There are four strategies that TAS acts on.
 
 **1 scheduleonmetric** has only one rule. It is consumed by the Telemetry Aware Scheduling Extender and prioritizes nodes based on a comparator and an up to date metric value.
  - example: **scheduleonmetric** when **cache_hit_ratio** is **GreaterThan**
  
 **2 dontschedule** strategy has multiple rules, each with a metric name and operator and a target. A pod with this policy will never be scheduled on a node breaking any one of these rules.
 - example: **dontschedule** if **gpu_usage** is **GreaterThan 10**
 
 **3 deschedule** is consumed by the extender. If a pod with this policy is running on a node that violates it can be descheduled with the kubernetes descheduler.
 - example: **deschedule** if **network_bandwidth_percent_free** is **LessThan 10**

 **4 labeling** is a multi-rule strategy for creating node labels based on rule violations. Multiple labels can be defined for each rule.
 The labels can then be used with external components.
 - example: **label 'gas-disable-card0'** if **gpu_card0_temperature** is **GreaterThan 100**

The policy definition section below describes how to actually create these strategies in a kubernetes cluster.

### Quick set up
The deploy folder has all of the yaml files necessary to get Telemetry Aware Scheduling running in a Kubernetes cluster. Some additional steps are required to configure the generic scheduler and metrics endpoints.

#### Custom Metrics Pipeline
TAS relies on metrics from the custom metrics pipeline. A guide on setting up the custom metrics pipeline to have it operate with TAS is [here.](docs/custom-metrics.md)
If this pipeline isn't set up, and node level metrics aren't exposed through it, TAS will have no metrics on which to make decisions.

#### Extender configuration
Note: a shell script that shows these steps can be found [here](deploy/extender-configuration). This script should be seen as a guide only, and will not work on most Kubernetes installations.

Note: [Configurator go tool](../configurator/README.md) does the same tasks as the script but provides backups, dry-run option and shows what modifications will be / were done.
<details>
<summary>Instructions for non-root <a href="https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/deploy/extender-configuration/configure-scheduler.sh">configure-scheduler.sh</a> script runners</summary>

>The `configure-scheduler.sh` script needs write access to under `/etc`. One typically either runs it as root, or you have to use sudo.
It also requires that sudo (or root) needs to have a working kubectl access to the cluster. If running `sudo kubectl version` looks ok,
you are likely good to go. If that produced an error, consider giving the folder `/root/.kube/` the cluster config file.
</details>&nbsp;

The extender configuration files can be found under deploy/extender-configuration.
TAS Scheduler Extender needs to be registered with the Kubernetes Scheduler. In order to do this a configuration file should be created like one the below:
````
apiVersion: kubescheduler.config.k8s.io/v1beta2
kind: KubeSchedulerConfiguration
clientConnection:
  kubeconfig: /etc/kubernetes/scheduler.conf
extenders:
  - urlPrefix: "https://tas-service.telemetry-aware-scheduling.svc.cluster.local:9001"
    prioritizeVerb: "scheduler/prioritize"
    filterVerb: "scheduler/filter"
    weight: 1
    enableHTTPS: true
    managedResources:
      - name: "telemetry/scheduling"
        ignoredByScheduler: true
    ignorable: true
    tlsConfig:
      insecure: false
      certFile: "/host/certs/client.crt"
      keyFile: "/host/certs/client.key"

````
This file can be found [in the deploy folder](deploy/extender-configuration/scheduler-config.yaml). The API version of the file is updated by executing a [shell script](deploy/extender-configuration/configure-scheduler.sh). 
Note that k8s, from version 1.22 onwards, will no longer accept a scheduling policy to be passed as a flag to the kube-scheduler. The shell script will make sure the scheduler is set-up according to its version: scheduling by policy or configuration file.
If scheduler is running as a service these can be added as flags to the binary. If scheduler is running as a container - as in kubeadm - these args can be passed in the deployment file.
Note: For Kubeadm set ups some additional steps may be needed.
1) Add the ability to get configmaps to the kubeadm scheduler config map. (A cluster role binding for this is at deploy/extender-configuration/configmap-getter.yaml)
2) Add the ``dnsPolicy: ClusterFirstWithHostNet`` in order to access the scheduler extender by service name.

After these steps the scheduler extender should be registered with the Kubernetes Scheduler.

#### Deploy TAS
Telemetry Aware Scheduling uses go modules. It requires Go 1.16+ with modules enabled in order to build. 
TAS current version has been tested with the recent Kubernetes version at the released date. It maintains support to the three most recent K8s versions. 
TAS was tested on Intel® Server Boards S2600WF and S2600WT-Based Systems.

A yaml file for TAS is contained in the deploy folder along with its service and RBAC roles and permissions. 
The TAS components all reside in a custom namespace: **telemetry-aware-scheduling**, which means this namespace needs to be created first:

``
kubectl create namespace telemetry-aware-scheduling
``

A secret called extender-secret will need to be created with the cert and key for the TLS endpoint. TAS will not deploy if there is no secret available with the given deployment file.

The secret can be created with:

``
kubectl create secret tls extender-secret --cert /etc/kubernetes/<PATH_TO_CERT> --key /etc/kubernetes/<PATH_TO_KEY> -n telemetry-aware-scheduling
``
<details>
<summary>Cert selection tip for <a href="https://github.com/intel/platform-aware-scheduling/blob/24f25a38613e326b4830f5e647211df16060fe70/telemetry-aware-scheduling/deploy/extender-configuration/configure-scheduler.sh#L136-L137">configure-scheduler.sh</a> users</summary>

>The `configure-scheduler.sh` script is hard-coded to use `/etc/kubernetes/pki/ca.key` and `/etc/kubernetes/pki/ca.crt`. The cert for the secret
must match the scheduler configuration, so use those files. If you instead want to use your own cert, you need to configure the scheduler to match.
</details>&nbsp;

Note: From K8s v24+ the control-plane node labels have changed, from `node-role.kubernetes.io/master` to `node-role.kubernetes.io/control-plane`.
This change affects how TAS gets deployed as the toleration and nodeAffinity rules have to be changed accordingly.
In order to provide support for future versions of K8s, both of these rules have been changed to make use of the `node-role.kubernetes.io/control-plane`
label [file](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/deploy/tas-deployment.yaml#L51-L62).
If you are running a version of Kubernetes **older that v1.24**, you will need to change both rules in the file above to use
`node-role.kubernetes.io/master`.  To see how this can be done automatically please see [this shell script](deploy/extender-configuration/configure-scheduler.sh)

In order to deploy run:

``kubectl apply -f deploy/``

After this is run TAS should be operable in the cluster and should be visible after running ``kubectl get pods``

Note: If you want to create the build and the image you can still do it by running ``make build && make image`` 
This will build locally the image ``tasextender``. Once created you may replace it into the deployment [file](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/deploy/tas-deployment.yaml#L28).

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

### Policy definition
A Telemetry Policy can be created in Kubernetes using ``kubectl apply -f`` on a valid policy file. 
The structure of a policy file is : 

````
apiVersion: telemetry.intel.com/v1alpha1
kind: TASPolicy
metadata:
  name: scheduling-policy
  namespace: health-metric-demo
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
    labeling:
      rules:
      - metricname: node_metric
        operator: GreaterThan
        target: 100
        labels: ["label1=foo","label2=bar"]
````
There can be four strategy types in a policy file and rules associated with each.
 - **scheduleonmetric** has only one rule. It is consumed by the Telemetry Aware Scheduling Extender and prioritizes nodes based on the rule.
 - **dontschedule** strategy has multiple rules, each with a metric name and operator and a target. A pod with this policy will never be scheduled on a node breaking any one of these rules.
 - **deschedule** is consumed by the extender. If a pod with this policy is running on a node that violates that pod can be descheduled with the kubernetes descheduler.
 - **labeling** is a multi-rule strategy for creating node labels based on rule violations. Multiple labels can be defined for each rule.
 The labels can then be used with external components.
 Labels will be written to the namespace `telemetry.aware.scheduling.policyname`, where policyname will be replaced by the name of the `TASPolicy`.
 For the above policy, if `node_metric` would be greater than 100, the created node labels would look like this:

         telemetry.aware.scheduling.scheduling-policy/label1=foo
         telemetry.aware.scheduling.scheduling-policy/label2=bar

     Labels should have different names in different rules. Labels are key-value pairs and only unique keys can exist in each label namespace.

     In the use of GreaterThan when using labels with the same name, the rule with the maximum metric value will be the only one honored.
     Similarly, in the use of LessThan when using labels with the same name, the rule with the minimum metric value will be the only one honored.
     Example:

         labeling:
           rules:
           - metricname: node_metric_1
             operator: GreaterThan
             target: 100
             labels: ["foo=1"]
           - metricname: node_metric_2
             operator: GreaterThan
             target: 100
             labels: ["foo=2"]

     The above rules would create label `telemetry.aware.scheduling.scheduling-policy/foo=1` when `node_metric_1` is greater than `node_metric_2` and also greater than 100.
     If instead `node_metric_2` would be greater than `node_metric_1` and also greater than 100, the produced label would be `telemetry.aware.scheduling.scheduling-policy/foo=2`.
     If neither metric would be greater than 100, no label would be created. When there are multiple candidates with equal values, the resulting label is
     random among the equal candidates. Label cleanup happens automatically. An example of the labeling strategy can be found in [here](docs/strategy-labeling-example.md)

Telemetry policies are namespaced, meaning that under normal circumstances a workload can only be associated with a pod in the same namespaces.   
dontschedule and deschedule strategies - which incorporate multiple rules - works with an OR operator (default value). That is if any single rule is broken the strategy is considered violated.
For the user-cases that request the use of other operators, the policy allows more descriptive operators such as `anyOf` and `allOf` which are equivalent to OR and AND operators, respectively. For example:

````
apiVersion: telemetry.intel.com/v1alpha1
kind: TASPolicy
metadata:
  name: multirules-policy
  namespace: health-metric-demo
spec:
  strategies:
    deschedule:
      logicalOperator: allOf
      rules:
      - metricname: temperature
        operator: GreaterThan
        target: 80
      - metricname: freeRAM
        operator: LessThan
        target: 200  
    dontschedule:
      logicalOperator: anyOf
      rules:
      - metricname: temperature
        operator: GreaterThan
        target: 80
      - metricname: freeRAM
        operator: LessThan
        target: 200  
    scheduleonmetric:
      rules:
      - metricname: freeRAM
        operator: LessThan
````
The deschedule strategy rule will be violated only if both metric rules are violated, while for dontschedule the violation will occur if one of the rules are broken. Note that the key:value map for the logicalOperator `anyOf` can be omitted, i.e., it has the same effect of the previous policy example (OR as default operator).  

### Configuration flags
The below flags can be passed to the binary at run time.

#### TAS Scheduler Extender
name |type | description| usage | default|
-----|------|-----|-------|-----|
|kubeConfig| string |location of kubernetes configuration file | -kubeConfig $PATH_TO_KUBE_CONFIG/config|$HOME/.kube/config
|syncPeriod|duration string| interval between refresh of telemetry data|-syncPeriod 1m| 1s
|port| int | port number on which the scheduler extender will listen| -port 32000 | 9001
|cert| string | location of the cert file for the TLS endpoint | --cert=/root/cert.txt| /etc/kubernetes/pki/ca.crt
|key| string | location of the key file for the TLS endpoint| --key=/root/key.txt | /etc/kubernetes/pki/ca.key
|cacert| string | location of the ca certificate for the TLS endpoint| --key=/root/cacert.txt | /etc/kubernetes/pki/ca.crt

## Linking a workload to a policy 
Pods can be linked with policies by adding a label of the form ``telemetry-policy=<POLICY-NAME>``
This also needs to be done inside higher level workload types i.e. deployments.

For example,  in a deployment file: 
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
TAS Scheduler Extender is set up to use in-Cluster config in order to access the Kubernetes API Server. When deployed inside the cluster this along with RBAC controls configured in the installation guide, will give it access to the required resources.
If outside the cluster TAS will try to use a kubernetes config file in order to get permission to get resources from the API server. This can be passed with the --kubeconfig flag to the binary.

When TAS Scheduler Extender contacts api server an identical flag  --kubeConfig can be passed if it's operating outside the cluster.
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

