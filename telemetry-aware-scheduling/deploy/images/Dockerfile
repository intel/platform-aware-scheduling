# Copyright (C) 2022 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

ARG GO_VERSION
FROM golang:${GO_VERSION} as builder
ARG GOLICENSES_VERSION
COPY . /src_root
WORKDIR /src_root/telemetry-aware-scheduling
ENV GOFLAGS -buildvcs=false
RUN mkdir -p /install_root/etc && adduser -D -u 10001 tas && tail -1 /etc/passwd > /install_root/etc/passwd \
    && CGO_ENABLED=0 GO111MODULE=on go build -ldflags="-s -w" -o /install_root/extender ./cmd \
    && GO111MODULE=on go run github.com/google/go-licenses@${GOLICENSES_VERSION} save "./cmd" --save_path /install_root/licenses

FROM scratch
WORKDIR /
COPY --from=builder /install_root /
EXPOSE 9001/tcp
USER tas
ENTRYPOINT ["/extender"]

HEALTHCHECK --interval=60s --start-period=30s --retries=5 CMD [ $(ps aux | grep "/extender --syncPeriod"  | wc -l) -eq 2 ] || exit 1
