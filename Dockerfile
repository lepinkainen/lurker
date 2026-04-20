FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o irc-service .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    mkdir -p /data && chown nobody:nobody /data

WORKDIR /app
COPY --from=builder /build/irc-service .

EXPOSE 8080
VOLUME ["/data"]
ENV DATA_DIR=/data
ENV CONFIG_PATH=/app/config.yaml
ENV ADDR=:8080

USER nobody
ENTRYPOINT ["./irc-service"]
