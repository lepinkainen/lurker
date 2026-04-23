FROM node:22-alpine AS web-builder

WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26-alpine AS builder

ARG VERSION=dev
ARG GIT_HASH=unknown
ARG BUILD_TIME=unknown

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.version=${VERSION} -X main.gitHash=${GIT_HASH} -X main.buildTime=${BUILD_TIME}" -o lurker .

FROM alpine:3.20

ARG VERSION=dev
ARG GIT_HASH=unknown
ARG BUILD_TIME=unknown

RUN apk add --no-cache ca-certificates tzdata wget && \
    mkdir -p /data /app/web/dist && chown nobody:nobody /data /app/web/dist

LABEL org.opencontainers.image.title="lurker" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_HASH}" \
      org.opencontainers.image.created="${BUILD_TIME}"

WORKDIR /app
COPY --from=builder /build/lurker .
COPY --from=builder /build/themes ./themes
COPY --from=web-builder /web/dist ./web/dist

EXPOSE 8080
VOLUME ["/data"]
ENV DATA_DIR=/data
ENV ADDR=:8080
ENV THEMES_DIR=/app/themes
ENTRYPOINT ["./lurker", "--web-dir", "/app/web/dist"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/whoami >/dev/null || exit 1

USER nobody
