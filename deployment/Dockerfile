# syntax=docker/dockerfile:1
# Infrastructure only: the mounted Darkrun binary updates independently.
FROM alpine:3.23
RUN apk add --no-cache ca-certificates curl jq unzip postgresql18-client
COPY --chmod=755 entrypoint.sh /usr/local/bin/darkrun-entrypoint
ENV DARKSPIN_BOOTSTRAP_PATH=/darkspin
USER 65532:65532
WORKDIR /darkspin
EXPOSE 42127/tcp 42127/udp
ENTRYPOINT ["/usr/local/bin/darkrun-entrypoint"]
CMD ["bootstrap"]
