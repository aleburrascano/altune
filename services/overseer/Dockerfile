# Overseer container image. Two build stages then a minimal runtime:
#   1. web:   Node builds the React + Vite SPA into internal/webui/dist.
#   2. build: Go embeds that dist via go:embed and compiles the static binary.
# The runtime image stays a minimal static Go binary (non-root, wget HEALTHCHECK),
# carrying none of go-api's media tooling — Overseer only watches over HTTP and
# serves the embedded UI.

# --- 1. Build the SPA -------------------------------------------------------
FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
# Vite's build.outDir writes to ../internal/webui/dist (see vite.config.ts), so the
# SPA lands at /src/internal/webui/dist for the Go stage to embed.
RUN npm run build

# --- 2. Build the Go binary (embeds the SPA) --------------------------------
FROM golang:1.26.6-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Drop the freshly built SPA into the embed dir (which otherwise holds only the
# committed .gitkeep sentinel) before the go:embed in internal/webui folds it into
# the binary.
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /overseer ./cmd/overseer

# --- 3. Runtime -------------------------------------------------------------
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
