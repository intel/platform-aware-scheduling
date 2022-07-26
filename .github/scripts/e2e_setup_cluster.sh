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
# running the latest available image my default, unless instructured to
KIND_IMAGE="kindest/node:v1.23.0@sha256:49824ab1727c04e56a21a5d8372a402fcd32ea51ac96a2706a12af38934f81ac"
[ -n "$1" ] && KIND_IMAGE=$1

# private registry set-up variables
CHANGE_MIRROR_REPO="false"
[ -n "$2" ] && CHANGE_MIRROR_REPO=$2
# private registry set-up directories and files
KIND_INTERNAL_CERTS_DIR="${root}/../../kind-e2e/ca-certificates"
REGISTRY_MIRROR_CONFIG_FILE="${root}/../../kind-e2e/registry-mirror.config"
KIND_SET_UP_CONFIG_TEMPLATE="${root}/.github/scripts/kind/config-template.yaml"
KIND_SET_UP_CONFIG_FILE="${root}/.github/scripts/kind/config.yaml"
UBUNTU_CERTS_DIR="/usr/local/share/ca-certificates/"

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

  # copy and fill in values in the template config file
  echo "Duplicating Kind cluster config template..."
  cp "$KIND_SET_UP_CONFIG_TEMPLATE" "$KIND_SET_UP_CONFIG_FILE"
  if [ ! -f "$KIND_SET_UP_CONFIG_FILE" ]; then
    echo "$KIND_SET_UP_CONFIG_FILE doesn't exist; Copy command above failed unexpectedly. Exiting..."
    exit 1
  fi
  echo "Done."
  echo "Updating Kind cluster config template with the corresponding parameters..."
  # update the mount_dir expressions. Using | for sed expecting mount_dir contains /
  sed -i "s|CP_MOUNT_DIR|${mount_dir:?}|g" "$KIND_SET_UP_CONFIG_FILE"
  sed -i "s|WORKER_MOUNT_DIR|$mount_dir|g" "$KIND_SET_UP_CONFIG_FILE"
 echo "Done."

  if [ "$CHANGE_MIRROR_REPO" == "true" ]; then
    echo "Update Kind cluster's containerd configuration with new mirror. This is for testing/CI purposes and is not meant for production."

    if [ ! -f "$REGISTRY_MIRROR_CONFIG_FILE" ]; then
      echo "$REGISTRY_MIRROR_CONFIG_FILE doesn't exist; this is needed for cluster containerd private registry config. Exiting..."
      exit 1
    fi
    MIRROR_ENDPOINT=$(< "$REGISTRY_MIRROR_CONFIG_FILE" cut -d "=" -f 2)
    {
      # adds new line
      echo ""
      echo 'containerdConfigPatches:'
      echo '  - |-'
      echo '    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]'
      echo "      endpoint = [$MIRROR_ENDPOINT]"
    } >> "$KIND_SET_UP_CONFIG_FILE"
  fi

  # deploy cluster with kind
  kind create cluster --image="$KIND_IMAGE"  --config="$KIND_SET_UP_CONFIG_FILE"

  # clean-up
  if [ -f "$KIND_SET_UP_CONFIG_FILE" ]; then
    echo "$KIND_SET_UP_CONFIG_FILE should be temporary. Will proceed to remove it..."
    rm "$KIND_SET_UP_CONFIG_FILE"
    echo "Removal complete."
  fi
}

install_certs_in_kind() {
  if [ "$CHANGE_MIRROR_REPO" == "true" ]; then
    # install the required certificates to access the internal image registry
    # the first kind is the default name of the cluster if you don't provide one, and -kind is appended afterwards by Kind
    echo "Will proceed to install the required certs in Kind for the private registry..."
    kind_cluster_name="kind-kind"
    kind_node_names="$(kubectl get nodes -o jsonpath='{.items[*].metadata.name}')"
    if [ -z "$kind_node_names" ]; then
       echo "No nodes found for the $kind_cluster_name  Kind cluster. Instead found: $kind_node_names. Exit..."
       exit 1
    fi

    read -ra kind_node_names_array <<< "$kind_node_names"

    for kind_node in "${kind_node_names_array[@]}"
    do
      echo "$kind_node"
      docker cp "$KIND_INTERNAL_CERTS_DIR/." "$kind_node:$UBUNTU_CERTS_DIR"
      # we need to run the remaining certificate install commands
      echo "Installing the necessary certificates for node $kind_node..."
      docker exec "$kind_node" update-ca-certificates
      # restart containerd on the node
      docker exec "$kind_node" systemctl restart containerd
    done
  fi
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
install_certs_in_kind

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
sed "s/intel\/telemetry-aware-scheduling/tasextender/g" "${root}/telemetry-aware-scheduling/deploy/tas-deployment.yaml" -i
kubectl apply -f "${root}/telemetry-aware-scheduling/deploy/"
