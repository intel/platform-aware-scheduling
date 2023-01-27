# Cluster API deployment

## Introduction

Cluster API is a Kubernetes sub-project focused on providing declarative APIs and tooling to simplify provisioning, upgrading, and operating multiple Kubernetes clusters. [Learn more](https://cluster-api.sigs.k8s.io/introduction.html).

This folder contains an automated and declarative way of deploying the Telemetry Aware Scheduler using Cluster API. We will make use of the [ClusterResourceSet feature](https://cluster-api.sigs.k8s.io/tasks/experimental-features/cluster-resource-set.html) to automatically apply a set of resources. Note you must enable its feature gate before running `clusterctl init` (with `export EXP_CLUSTER_RESOURCE_SET=true`).

## Guides

- [Cluster API deployment - Docker provider (for local testing/development only)](docker/capi-docker.md)
- [Cluster API deployment - Generic provider](generic/capi.md)

## Testing

You can test if the scheduler actually works by following this guide: 
[Health Metric Example](https://github.com/intel/platform-aware-scheduling/blob/master/telemetry-aware-scheduling/docs/health-metric-example.md)