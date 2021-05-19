#!/bin/bash
# Remove any test artifacts created by tests
set -o errexit

root="$(dirname "$0")/../../"
tmp_dir="${root:?}/tmp"

echo "removing '${tmp_dir}'" 
rm -rf --preserve-root "${tmp_dir:?}" 
