
FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.6-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /overseer ./cmd/overseer

FROM alpine:3.21
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 1000 overseer

RUN mkdir -p /var/lib/overseer \
    && chown overseer:overseer /var/lib/overseer \
    && chmod 700 /var/lib/overseer

USER overseer
COPY --from=build /overseer /overseer

HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8090/health || exit 1

ENTRYPOINT ["/overseer"]
