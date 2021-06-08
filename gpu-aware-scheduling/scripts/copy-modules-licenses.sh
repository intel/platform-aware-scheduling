#!/bin/sh
#
# Copyright 2019-2021 Intel Corporation.
#
# SPDX-License-Identifier: Apache-2.0
#
# Copy the license obligations of ".Deps" modules for a package to a target directory
set -o errexit
set -o nounset

if [ $# != 2 ] || [ "$1" = "?" ] || [ "$1" = "--help" ]; then
	echo "Usage: $0 <package> <target dir>" >&2
	exit 1
fi

if [ ! -d "$2" ] || [ ! -w "$2" ]; then
	echo "Error: cannot use $2 as the target directory"
	exit 1
fi

if [ ! -d "$2"/package-licenses ]; then
	mkdir "$2"/package-licenses
fi

export GO111MODULE=on

if [ ! -d vendor ]; then
	go mod vendor -v
fi

LICENSE_FILES=$(find vendor |grep -e LICENSE -e NOTICE|cut -d / -f 2-)
PACKAGE_DEPS=$(go list -f '{{ join .Deps "\n" }}' "$1" |grep "\.")

POPD=$(pwd)
cd vendor

for lic in $LICENSE_FILES; do
	DIR=$(dirname "$lic")

	# Copy the license if its repository path is found in package .Deps
	if [ "$(echo "$PACKAGE_DEPS" | grep -c "$DIR")" -gt 0 ]; then
		cp --parents "$lic" "$2"/package-licenses

		# Copy the source if the license is MPL/GPL/LGPL
		if [ "$(grep -c -w -e MPL -e GPL -e LGPL "$lic")" -gt 0 ]; then
			if [ ! -d "$2"/package-sources ]; then
				mkdir "$2"/package-sources
			fi
			tar -zvcf  "$2"/package-sources/"$(echo "$DIR" | tr / _)".tar.gzip "$DIR"
		fi
	fi
done

cd "$POPD"
