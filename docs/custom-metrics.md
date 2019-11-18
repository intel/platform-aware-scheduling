# Custom Metrics Pipeline

Telemetry Aware Scheduling is designed to be deployed as a pod on a Kubernetes master. It is designed to pull metrics from the Kubernetes Custom Metrics API. This API has no data by default. There are various Custom Metrics Pipelines that can be implemented in order to make that data available. Our example deployment is based on [Prometheus](https://github.com/prometheus/prometheus) the CNCF time series database.

The three components to make platform metrics available via the Custom Metrics API are:
- **Prometheus CNCF** time series data base
- **Prometheus Node Exporter** which makes platform metrics available to Prometheus
- **Prometheus Adapter** which makes metrics from Prometheus available in the Custom Metrics API

Yaml files for the installation of the below components are available from [here](https://github.com/coreos/kube-prometheus)
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

### Node exporter
Prometheus Node Exporter is available from its project repo https://github.com/prometheus/node_exporter

Once installed as a daemonset on the cluster it will start to export metrics to prometheus. With the above scrape config prometheus will label each of these as a node metric prefixing them with node. This prefix is in turn used by the custom metrics adapter to pick up the platform metrics as node metrics

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
Once this is done metrics should start to appear at the metrics endpoint. The following command should show a group of metrics objects.
 
``
kubectl get --raw /apis/custom.metrics.k8s.io/v1beta1 | grep nodes | jq
``

**Note** jq is a json formatter. If not installed it can be excluded from this command.

Once this is done TAS will be able to pick up any metric from this endpoint using its name.