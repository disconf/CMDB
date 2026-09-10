FROM harbor.qa.bcavt.com/library/prom/node-exporter:latest AS node-exporter

FROM golang:1.26-alpine AS build
WORKDIR /src
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 65532 nonroot \
    && adduser -S -D -H -u 65532 -G nonroot nonroot
COPY --from=build --chown=65532:65532 /out/gateway /gateway
COPY --from=node-exporter --chown=65532:65532 /bin/node_exporter /node_exporter
EXPOSE 8080
USER 65532:65532
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 CMD ["/gateway", "healthcheck"]
ENTRYPOINT ["/gateway"]
