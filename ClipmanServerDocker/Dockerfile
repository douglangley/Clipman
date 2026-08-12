FROM golang:1.26-alpine AS build

WORKDIR /src
COPY ClipmanServer/go.mod ClipmanServer/go.sum ./
RUN go mod download
COPY ClipmanServer/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/clipman-server ./cmd/clipman-server

FROM alpine:3.22

LABEL org.opencontainers.image.title="Clipman Server"
LABEL org.opencontainers.image.description="Self-hosted sync server for Clipman clipboard history databases."
LABEL org.opencontainers.image.source="https://github.com/OnjLouis/Clipman"

ENV CLIPMAN_DATA_DIR=/data
ENV CLIPMAN_HOST=0.0.0.0
ENV CLIPMAN_PORT=8080

COPY --from=build /out/clipman-server /usr/local/bin/clipman-server
COPY ClipmanServerDocker/docker-entrypoint.sh /usr/local/bin/clipman-server-entrypoint
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 clipman \
    && adduser -S -D -H -u 10001 -G clipman clipman \
    && mkdir -p /data \
    && chown clipman:clipman /data \
    && chmod +x /usr/local/bin/clipman-server /usr/local/bin/clipman-server-entrypoint

VOLUME ["/data"]
EXPOSE 8080

USER 10001:10001
ENTRYPOINT ["clipman-server-entrypoint"]
