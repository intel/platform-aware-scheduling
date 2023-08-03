# Copyright (C) 2023 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

ARG GO_VERSION
FROM golang:${GO_VERSION} as builder
ARG GOLICENSES_VERSION
ARG LOCAL_LICENSES
COPY . /src_root
WORKDIR /src_root/configurator
RUN mkdir -p /install_root/bin && BUILD_OUTPUT_DIR=/install_root/bin make build \
    && if [ -z "$LOCAL_LICENSES" ] ; then \
    GO111MODULE=on go run github.com/google/go-licenses@${GOLICENSES_VERSION} save "./internal/cmd" --save_path /install_root/licenses ; \
    else cp -r licenses /install_root/ ; fi

WORKDIR /src_root
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN mkdir -p /yamls && find . -name '*.yaml' | grep deploy | grep -v -E ".github|demo|/charts/" | xargs -I{} cp --parents {} /yamls/

FROM scratch
# USER intentionally not specified. This is expected to write to /etc in the normal case.
WORKDIR /
COPY --from=builder /install_root /
COPY --from=builder /yamls/ /
ENTRYPOINT ["/bin/configurator"]
