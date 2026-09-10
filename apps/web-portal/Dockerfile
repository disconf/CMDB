FROM node:24-alpine AS web-build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM golang:1.26-alpine AS server-build
WORKDIR /src
COPY go.mod ./
COPY server.go ./
COPY --from=web-build /app/dist ./dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /web-static . && chmod 0755 /web-static

FROM scratch
COPY --from=server-build /web-static /web-static
USER 65532:65532
ENTRYPOINT ["/web-static"]
