FROM golang:1.26-alpine AS builder

ARG VERSION=dev
ARG GIT_HASH=unknown
ARG BUILD_TIME=unknown

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.version=${VERSION} -X main.gitHash=${GIT_HASH} -X main.buildTime=${BUILD_TIME}" -o irc-service .

FROM alpine:3.20

ARG VERSION=dev
ARG GIT_HASH=unknown
ARG BUILD_TIME=unknown

RUN apk add --no-cache ca-certificates tzdata && \
    mkdir -p /data && chown nobody:nobody /data

LABEL org.opencontainers.image.title="irc-service" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_HASH}" \
      org.opencontainers.image.created="${BUILD_TIME}"

WORKDIR /app
COPY --from=builder /build/irc-service .

EXPOSE 8080
VOLUME ["/data"]
ENV DATA_DIR=/data
ENV CONFIG_PATH=/app/config.yaml
ENV ADDR=:8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/whoami >/dev/null || exit 1

USER nobody
ENTRYPOINT ["./irc-service"]
