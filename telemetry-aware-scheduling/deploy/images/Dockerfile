#
# Copyright (c) 2022 Intel Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
FROM golang:1.17-alpine as user_builder
RUN adduser -D -u 10001 tas

FROM golang:1.17-alpine as builder
ARG DIR=telemetry-aware-scheduling
ARG SRC_ROOT=/src_root
COPY . ${SRC_ROOT}

RUN mkdir -p /install_root/etc
COPY --from=user_builder /etc/passwd /install_root/etc/passwd

WORKDIR ${SRC_ROOT}/${DIR}
RUN CGO_ENABLED=0 GO111MODULE=on go build -ldflags="-s -w" -o /install_root/extender ./cmd \
    && install -D ${SRC_ROOT}/${DIR}/LICENSE /install_root/usr/local/share/package-licenses/telemetry-aware-scheduling/LICENSE \
    && scripts/copy-modules-licenses.sh ./cmd /install_root/usr/local/share/

FROM scratch
WORKDIR /
COPY --from=builder /install_root /
EXPOSE 9001/tcp
USER tas
ENTRYPOINT ["/extender"]
