FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cmdb-agent .
FROM alpine:3.22
RUN adduser -S -D -H -u 10001 agent
COPY --from=build /out/cmdb-agent /usr/local/bin/cmdb-agent
USER agent
ENTRYPOINT ["/usr/local/bin/cmdb-agent"]
