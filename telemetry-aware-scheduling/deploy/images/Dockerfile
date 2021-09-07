FROM golang:1.16-alpine as user_builder
RUN adduser -D -u 10001 tas

FROM golang:1.16-alpine as builder
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