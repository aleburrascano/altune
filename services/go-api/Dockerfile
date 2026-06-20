FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app ./cmd/api

FROM alpine:3.21
RUN apk add --no-cache \
    ca-certificates \
    ffmpeg \
    nodejs \
    python3 \
    py3-pip \
    && pip3 install --no-cache-dir --break-system-packages yt-dlp \
    && adduser -D -u 1000 altune

USER altune
COPY --from=build /app /app
ENTRYPOINT ["/app"]
