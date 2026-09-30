# syntax=docker/dockerfile:1
FROM node:24.14.1-bookworm-slim AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27.0-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -o /out/jev-router ./cmd/jev-router

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir /data && chown 10001:10001 /data
COPY --from=build /out/jev-router /usr/local/bin/jev-router
USER 10001:10001
WORKDIR /data
ENV JEV_ROUTER_LISTEN=0.0.0.0:8080 \
    JEV_ROUTER_DATABASE=/data/router.sqlite \
    JEV_ROUTER_URL=http://127.0.0.1:8080
EXPOSE 8080
ENTRYPOINT ["jev-router"]
CMD ["serve"]
