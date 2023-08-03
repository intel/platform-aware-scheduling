# Copyright (C) 2022 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

ARG GO_VERSION
FROM golang:${GO_VERSION} as builder
ARG GOLICENSES_VERSION
ARG LOCAL_LICENSES
COPY . /src_root
WORKDIR /src_root/gpu-aware-scheduling
RUN mkdir -p /install_root/etc && adduser --disabled-password --quiet --gecos "" -u 10001 gas && tail -1 /etc/passwd > /install_root/etc/passwd \
    && BUILD_OUTPUT_DIR=/install_root make build \
    && if [ -z "$LOCAL_LICENSES" ] ; then \
    GO111MODULE=on go run github.com/google/go-licenses@${GOLICENSES_VERSION} save "./cmd/gas-scheduler-extender" --save_path /install_root/licenses ; \
    else cp -r licenses /install_root/ ; fi

FROM scratch
WORKDIR /
COPY --from=builder /install_root /
EXPOSE 9001/tcp
USER gas
ENTRYPOINT ["/extender"]
