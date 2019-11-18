FROM alpine:latest
RUN adduser -D -u 10001 tas

FROM scratch
COPY controller .
COPY --from=0 /etc/passwd /etc/passwd
USER tas
ENTRYPOINT ["/controller"]
