#!/bin/bash
# Teardown Kind cluster

root="$(dirname "$0")/../../"
export PATH="${PATH}:${root:?}/tmp/bin"

if ! command -v kind &> /dev/null; then
  echo "kind is not available. Run 'make e2e' first"
  exit 1
fi

kind delete cluster
