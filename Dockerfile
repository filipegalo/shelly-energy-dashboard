# syntax=docker/dockerfile:1.7
FROM node:22-alpine AS assets
WORKDIR /src
COPY package.json package-lock.json tailwind.config.js ./
COPY scripts ./scripts
COPY internal/web/templates ./internal/web/templates
COPY internal/web/static ./internal/web/static
RUN npm ci --ignore-scripts && npm run assets

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=assets /src/internal/web/static/css/tailwind.css ./internal/web/static/css/tailwind.css
COPY --from=assets /src/internal/web/static/js/htmx.min.js ./internal/web/static/js/htmx.min.js
COPY --from=assets /src/internal/web/static/js/chart.umd.js ./internal/web/static/js/chart.umd.js
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/shelly-inventory ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache tzdata && addgroup -S shelly && adduser -S -G shelly -h /data shelly
COPY --from=build /out/shelly-inventory /usr/local/bin/shelly-inventory
RUN mkdir -p /data && chown shelly:shelly /data && chmod 0700 /data
USER shelly
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/usr/local/bin/shelly-inventory", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/shelly-inventory"]
