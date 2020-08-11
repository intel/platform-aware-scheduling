## DPDK busyness metrics inside a Kubernetes deployment

This guide will show how to use the dpdk example app l3fwd-power in order to access the special "busyness" indicators from a dpdk workload. The guide will involve first building an image based on DPDK 20.05, then deploying the image as a Kubernetes Pod. Finally the workload is set up and metrics queries to show the availability of these metrics.

This set up relies on Kubernetes having core isolation offered by the Kubernetes CPU Manager. Additionally the SR-IOV stack for Kubernetes is used to advertise and provide userspace (VFIO driver) Virtual Network functions into the pod.

1) Build the docker container
The following Dockerfile DPDK on top of centos8 and then installs the l3fwd application which is not build by default.

Create it using: 
```docker build . -it dpdkcollectdl3fwd```

2) Deploy a pod using the container

This will create a pod that uses SR-IOV virtual functions and pinned cores in order to provide the environment for a DPDK application. To create the Pod run:

```kubeclt apply -f dpdkcollectdl3fwd.yml```

Note this assumes that the pod will run on the same host as the image was built. If this is not the case the image will need to be made available on more nodes, either by building it there or putting it in a registry.

3) Run the workload

In order to run l3fwd a bunch of information needs to be passed to the pod including the interfaces its able to use and the cpu cores that it has pinned to it. Information on the cpu cores is available from /var/lib/kubelet/cpu_manager_state on the node the workload is running. Device information for the SR-IOV VFs can be parsed from the environment inside the running pod. The run.sh script in this folder gets the devices, but the cpus are hard coded so they will need to be altered with information from the cpu manager state file.

4) Send traffic to the application
To be added in a future guide - preferably without needing IXIA.
5) Show metrics

As created in Kubernetes this application will directly expose metrics on the container - but these metrics are not automatically available from Prometheus or other upstream components. A future version of this guide will show how to expose those metrics. To see the metrics on the pod run:

``` kubectl exec -it dpdkcollectdl3fwd -- wget -O- localhost:9103 ```


This should return a page of metrics including busyness metrics if the l3fwd-power application is running. To confirm run: 

``` kubeclt exec -it dpdkcollectdl3fwd -- wget -o- localhost:9103 | grep busy```   
