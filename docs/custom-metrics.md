 Custom Metrics Pipeline

Telemetry Aware Scheduling is designed to be deployed as a pod on a Kubernetes master. It is designed to pull metrics from the Kubernetes Custom Metrics API. This API has no data by default. There are various Custom Metrics Pipelines that can be implemented in order to make that data available. Our example deployment is based on [Prometheus](https://github.com/prometheus/prometheus) the CNCF time series database.

The three components to make platform metrics available via the Custom Metrics API are:
- **Prometheus:** CNCF time series data base
- **Prometheus Node Exporter:** which makes platform metrics available to Prometheus
- **Prometheus Adapter:** which makes metrics from Prometheus available in the Custom Metrics API

## Quick install
This install requires the use of Helm 3.0.

Helm charts are included in the deploy/charts directory which will allow the preconfigured installation of the custom metrics pipeline.

In order to install Node Exporter and Prometheus run:
````
kubectl create namespace monitoring
helm install node-exporter deploy/charts/prometheus_node_exporter_helm_chart/
helm install prometheus deploy/charts/prometheus_helm_chart/
````
At this stage Node Exporter, which is deployed as a daemonset, should be advertising metrics on port 9100 on each node. The prometheus dashboard should be available at port 30000 for the entire cluster.

The custom metrics adapter requires tls to be configured with a cert and key.
Information on how to generate correctly signed certs in kubernetes can be found [here](https://github.com/kubernetes-sigs/apiserver-builder-alpha/blob/master/docs/concepts/auth.md)
Files ``serving-ca.crt`` and ``serving-ca.key`` should be in the working directory when running the below commands.

````
kubectl create namespace custom-metrics
kubectl -n custom-metrics create secret tls cm-adapter-serving-certs --cert=serving-ca.crt --key=serving-ca.key
helm install prometheus-adapter deploy/charts/prometheus_custom_metrics_helm_chart/
````

The Prometheus adapter may take some time to come online - but once it does running:
``kubectl get --raw /apis/custom.metrics.k8s.io/v1beta1 | grep nodes | jq .``
should return a number of metrics that are being collected by prometheus node exporter, scraped by prometheus and passed to the Kubernetes Metrics API by the Prometheus Adapter.

In order to be certain the raw metrics are available look at a specific endpoint output from the above command e.g.
``kubectl get --raw /apis/custom.metrics.k8s.io/v1beta1/nodes/*/health_metric | jq .``

Where "health_metric" is the specific metric looked at. The output should be a json object with names an metrics info for each node in the cluster.

## Configuration guide 
What follows is a description of the configurations for each component required to deliver metrics to TAS. 
If you have used the above helm charts to deploy this configuration is already applied. 
### Prometheus 
Prometheus can be installed from the projects repo https://github.com/prometheus/prometheus.

The prometheus config map should have a scrape config like the below included to scrape correctly from node exporter. 

````
      - job_name: 'kubernetes-node-exporter'
        tls_config:
          ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
        bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
        scheme: http
        kubernetes_sd_configs:
        - role: node
        relabel_configs:
        - source_labels: [__address__]
          regex: ^(.*):\d+$
          target_label: __address__
          replacement: $1:9100
          # Host name
        - source_labels: [__meta_kubernetes_node_name]
          target_label: node

````
This scrape config references the location of the Node Exporter metrics ($NODE_NAME:9100) and tells prometheus how to label these metrics i.e. as a Kubernetes node object.
More on how Prometheus scrape configs can be used and customized can be found at the project's [site](https://prometheus.io/docs/prometheus/latest/configuration/configuration/)
### Node exporter
Prometheus Node Exporter is available from its project repo https://github.com/prometheus/node_exporter

Once installed as a daemonset on the cluster it will start to export metrics to prometheus. With the above scrape config prometheus will label each of these as a node metric prefixing them with node. This prefix is in turn used by the custom metrics adapter to pick up the platform metrics as node metrics.
Node exporter labels each metric collected by it with a label ``instance="$NODE_NAME"`` which allows it to be correctly linked to a specific node in the cluster.

### Prometheus adapter
Prometheus adapter can be installed from its repo https://github.com/DirectXMan12/k8s-prometheus-adapter
The following should be added in the config map for the prometheus adapter as a scrape configuration.
````
  data:
    config.yaml: |
      rules:
      - seriesQuery: '{__name__=~"^node_.*"}'
        resources:
          overrides:
            instance:
              resource: node
        name:
          matches: ^node_(.*)
        metricsQuery: <<.Series>>

````
The series query here picks up any metric that begins with node_ and marks it as a Kubernetes node in the Metrics API.
This labelling - along with the labels applied by node exporter-  is what allows TAS to access platform metrics as individual node metrics
