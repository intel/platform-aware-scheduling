FROM alpine:3.12.1 as build
RUN adduser -D -u 10001 tas

FROM scratch
COPY extender .
COPY --from=build /etc/passwd /etc/passwd
EXPOSE 9001/tcp
USER tas
ENTRYPOINT ["/extender"]
