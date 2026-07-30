# syntax=docker/dockerfile:1
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/torrentguard ./cmd/torrentguard

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S torrentguard \
    && adduser -S -G torrentguard -h /app torrentguard \
    && mkdir -p /data \
    && chown torrentguard:torrentguard /data
COPY --from=build /out/torrentguard /usr/local/bin/torrentguard
USER torrentguard
VOLUME ["/data"]
EXPOSE 8080
ENV DATABASE_PATH=/data/torrentguard.db \
    LISTEN_ADDRESS=:8080 \
    ACTION_MODE=observe \
    DRY_RUN=true
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["/usr/local/bin/torrentguard"]
