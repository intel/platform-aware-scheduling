#!/bin/bash
set -eo pipefail

root="$(dirname "$0")/../../"
export PATH="${PATH}:${root:?}/bin:${root:?}/tmp/bin"
RETRY_MAX=10
INTERVAL=10
TIMEOUT=300
APP_NAME="tasextender"
APP_DOCKER_TAG="${APP_NAME}:latest"
K8_ADDITIONS_PATH="${root}/.github/scripts/policies"
TMP_DIR="${root}/tmp"
CNIS_DAEMONSET_URL="https://raw.githubusercontent.com/intel/multus-cni/master/e2e/cni-install.yml"
CNIS_NAME="cni-plugins"

# create cluster CA and policy for Kubernetes Scheduler
# CA cert & key along with will be mounted to control plane
# path /etc/kubernetes/pki. Kubeadm will utilise generated CA cert/key as root
# Kubernetes CA. Cert for scheduler/TAS will be signed by this CA
generate_k8_scheduler_config_data() {
  mkdir -p "${TMP_DIR}"
  mount_dir="$(mktemp -q -p "${TMP_DIR}" -d -t tas-e2e-k8-XXXXXXXX)"
  cp "${K8_ADDITIONS_PATH}/policy.yaml" "${mount_dir}/"
}

create_cluster() {
  [ -z "${mount_dir}" ] && echo "### no mount directory set" && exit 1
  # deploy cluster with kind
  cat <<EOF | kind create cluster --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
kubeadmConfigPatches:
- |
  kind: ClusterConfiguration
  scheduler:
    dnsPolicy: ClusterFirstWithHostNet
    extraArgs:
      config: /etc/kubernetes/policy/policy.yaml
    extraVolumes:
    - name: kubeconfig
      hostPath: /etc/kubernetes/scheduler.conf
      mountPath: /etc/kubernetes/scheduler.conf 
    - name: certs 
      hostPath: /etc/kubernetes/pki/
      mountPath: /etc/kubernetes/pki/
    - name: schedulerconfig
      hostPath: /etc/kubernetes/policy/policy.yaml
      mountPath: /etc/kubernetes/policy/policy.yaml
nodes:
  - role: control-plane
    extraMounts:
    - hostPath: "${mount_dir:?}"
      containerPath: "/etc/kubernetes/policy/"
  - role: worker
    extraMounts:
    - hostPath: "${mount_dir}/node1"
      containerPath: "/tmp/node-metrics/node1.prom"
      propagation: HostToContainer
  - role: worker
    extraMounts:
    - hostPath: "${mount_dir}/node2"
      containerPath: "/tmp/node-metrics/node2.prom"
      propagation: HostToContainer
  - role: worker
    extraMounts:
    - hostPath: "${mount_dir}/node3"
      containerPath: "/tmp/node-metrics/node3.prom"
      propagation: HostToContainer

EOF
}

retry() {
  local status=0
  local retries=${RETRY_MAX:=5}
  local delay=${INTERVAL:=5}
  local to=${TIMEOUT:=20}
  cmd="$*"

  while [ $retries -gt 0 ]
  do
    status=0
    timeout $to bash -c "echo $cmd && $cmd" || status=$?
    if [ $status -eq 0 ]; then
      break;
    fi
    echo "Exit code: '$status'. Sleeping '$delay' seconds before retrying"
    sleep "$delay"
    retries=$((retries-1))
  done
  return $status
}

check_requirements() {
  for cmd in docker kind openssl kubectl base64; do
    if ! command -v "$cmd" &> /dev/null; then
      echo "$cmd is not available"
      exit 1
    fi
  done
}

echo "## checking requirements"
check_requirements
# generate K8 API server CA key/cert and supporting files for mTLS with NRI
echo "## generating K8s scheduler config"
generate_k8_scheduler_config_data


echo "## copy node metrics files to mount path"
cp "${K8_ADDITIONS_PATH}/node1" "${mount_dir}"
cp "${K8_ADDITIONS_PATH}/node2" "${mount_dir}"
cp "${K8_ADDITIONS_PATH}/node3" "${mount_dir}"


echo "## start Kind cluster with precreated CA key/cert"
create_cluster



kubectl create namespace monitoring;
helm install node-exporter "${root}/telemetry-aware-scheduling/deploy/charts/prometheus_node_exporter_helm_chart/";


helm install prometheus "${root}/telemetry-aware-scheduling/deploy/charts/prometheus_helm_chart/";
docker exec kind-control-plane mkdir -p /tmp/node-metrics/;

openssl req -x509 -sha256 -new -nodes -days 365 -newkey rsa:2048 -keyout  "${TMP_DIR}/serving-ca.key" -out "${TMP_DIR}/serving-ca.crt" -subj "/CN=ca";
kubectl create namespace custom-metrics ;kubectl -n custom-metrics create secret tls cm-adapter-serving-certs --cert="${TMP_DIR}/serving-ca.crt" --key="${TMP_DIR}/serving-ca.key";
helm install prometheus-adapter "${root}/telemetry-aware-scheduling/deploy/charts/prometheus_custom_metrics_helm_chart/"

echo "## build TAS"
retry make build
retry make image
echo "## load TAS image into Kind"
kind load docker-image "${APP_DOCKER_TAG}"

echo "## config for kube-scheduler dns"
docker cp  kind-control-plane:/etc/kubernetes/manifests/kube-scheduler.yaml  "${TMP_DIR}/kube-scheduler.yaml" 

sed -e "/spec/a\\
  dnsPolicy: ClusterFirstWithHostNet" "${TMP_DIR}/kube-scheduler.yaml" -i


docker cp "${TMP_DIR}/kube-scheduler.yaml" kind-control-plane:/etc/kubernetes/manifests/kube-scheduler.yaml
echo "## install coreDNS"
kubectl -n kube-system wait --for=condition=available deploy/coredns --timeout=300s
echo "## install CNIs"
retry kubectl create -f "${CNIS_DAEMONSET_URL}"
retry kubectl -n kube-system wait --for=condition=ready -l name="${CNIS_NAME}" pod --timeout=300s


mkdir "${mount_dir}/certs"
docker cp kind-control-plane:/etc/kubernetes/pki/ca.crt "${mount_dir}/certs/client.crt"
docker cp kind-control-plane:/etc/kubernetes/pki/ca.key "${mount_dir}/certs/client.key"


kubectl create secret tls extender-secret --cert "${mount_dir}/certs/client.crt" --key "${mount_dir}/certs/client.key"
kubectl apply -f "${root}/telemetry-aware-scheduling/deploy/"
