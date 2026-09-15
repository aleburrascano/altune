# Overseer container image. Mirrors services/go-api/deploy/Dockerfile (same
# builder, static CGO-free binary, non-root user, wget HEALTHCHECK) but carries
# none of go-api's media tooling: Overseer only watches over HTTP and serves an
# embedded UI, so the runtime stays minimal.
FROM golang:1.26.6-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /overseer ./cmd/overseer

FROM alpine:3.21
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 1000 overseer

USER overseer
COPY --from=build /overseer /overseer

# 8090 is Overseer's default port (OVERSEER_PORT). /health is open so an off-box
# uptime check can confirm Overseer is alive even when the watched app is down.
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8090/health || exit 1

ENTRYPOINT ["/overseer"]
