# Floating LTS tag, deliberately unpinned: it always resolves to the Active LTS
# line, which is the policy for every Node version in this repo. CI uses
# node-version: 'lts/*' for the same reason. Dependabot has no concept of LTS and
# would offer whatever Current release is newest (that is how node 26 briefly
# landed here), so there is no version string left for it to bump.
#
# The cost is that a new LTS major arrives silently, with no PR and no diff. If
# the frontend build breaks with nothing in the repo having changed, check
# whether Node's Active LTS just rolled -- the next roll is 2026-10-28 (v26).
# Node only runs in this build stage and in CI; the shipped image is the Go
# binary plus web/dist, so a bad major fails the build rather than production.
FROM node:lts-alpine AS web-builder

WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.27-alpine AS builder

ARG VERSION=dev
ARG GIT_HASH=unknown
ARG BUILD_TIME=unknown

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.version=${VERSION} -X main.gitHash=${GIT_HASH} -X main.buildTime=${BUILD_TIME}" -o lurker .

FROM alpine:3.24

ARG VERSION=dev
ARG GIT_HASH=unknown
ARG BUILD_TIME=unknown

RUN apk add --no-cache ca-certificates tzdata wget && \
    mkdir -p /data /app/web/dist && chown nobody:nobody /data /app/web/dist

LABEL org.opencontainers.image.title="lurker" \
      org.opencontainers.image.source="https://github.com/lepinkainen/lurker" \
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
