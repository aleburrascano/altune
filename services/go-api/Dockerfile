FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app ./cmd/api

FROM alpine:3.21
# yt-dlp pinned (not `pip3 install yt-dlp`): Docker caches this layer across
# rebuilds since nothing upstream of it changes, so an unpinned install goes
# stale silently even through redeploys. YouTube breaks old yt-dlp versions
# (signature/cipher changes -> HTTP 403 on download) every few weeks — bump
# this pin when downloads start failing with "unable to download video data".
RUN apk add --no-cache \
    ca-certificates \
    ffmpeg \
    nodejs \
    python3 \
    py3-pip \
    && pip3 install --no-cache-dir --break-system-packages yt-dlp==2026.7.4 \
    && adduser -D -u 1000 altune

USER altune
COPY --from=build /app /app
ENTRYPOINT ["/app"]
