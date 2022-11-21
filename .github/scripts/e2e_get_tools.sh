#!/bin/bash
set -o errexit

root="$(dirname "$0")/../../"

VERSION=""
[ -n "$1" ] && VERSION=$1
if [ -z "$VERSION" ]; then
    echo "Kind version is required, but found $VERSION. Exiting..."
    exit 1
fi

KIND_BINARY_URL="https://github.com/kubernetes-sigs/kind/releases/download/${VERSION}/kind-$(uname)-amd64"
K8_STABLE_RELEASE_URL="https://storage.googleapis.com/kubernetes-release/release/stable.txt"

HELM_STABLE_RELEASE_URL="https://get.helm.sh/helm-v3.5.4-linux-amd64.tar.gz"

if [ ! -d "${root:?}/tmp/bin" ]; then
    mkdir -p "${root:?}/tmp/bin"
fi

echo "retrieving kind"
curl --max-time 10 --retry 10 --retry-delay 5 --retry-max-time 60 -Lo "${root}/tmp/bin/kind" "${KIND_BINARY_URL}"
chmod +x "${root}/tmp/bin/kind"

echo "retrieving kubectl"
curl -Lo "${root}/tmp/bin/kubectl" "https://storage.googleapis.com/kubernetes-release/release/$(curl -s ${K8_STABLE_RELEASE_URL})/tmp/bin/linux/amd64/kubectl"
chmod +x "${root}/tmp/bin/kubectl"


echo "retrieving helm"
curl --max-time 10 --retry 10 --retry-delay 5 --retry-max-time 60 -Lo "${root}/tmp/helm.tar.gz" "${HELM_STABLE_RELEASE_URL}" 
tar -zxvf "${root}/tmp/helm.tar.gz" && mv linux-amd64/helm "${root}/tmp/bin/helm" && rm -rf linux-amd64
chmod +x "${root}/tmp/bin/helm"

IS_LOCAL="false"
[ -n "$2" ] && IS_LOCAL=$2
if [ "$IS_LOCAL" == "true" ]; then
  PROJECT_ROOT_DIR="${root}/../.."
  KIND_INTERNAL_CERTS_DIR="${PROJECT_ROOT_DIR}/kind-e2e/ca-certificates"
  # We expect certificates to be already present on the host, in this specific folder. If not exit with an error
  if  [ ! -d "$KIND_INTERNAL_CERTS_DIR" ] || [ -z "$(ls -A "$KIND_INTERNAL_CERTS_DIR")" ]; then
    echo "$KIND_INTERNAL_CERTS_DIR doesn't exist or it's empty. Please check host set-up process as this should've happened on runner set-up. Exiting...  "
    exit 1
  else
    echo "$KIND_INTERNAL_CERTS_DIR is not empty. Will continue and will use existing certificates..."
  fi
fi


