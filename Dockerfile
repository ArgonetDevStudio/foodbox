# syntax=docker/dockerfile:1.7

FROM node:24-alpine AS frontend-build

WORKDIR /src/front

COPY front/package.json front/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci

COPY front/ ./
RUN npm run build


FROM golang:1.26.5-alpine AS backend-build

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY backend/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -trimpath -ldflags="-s -w" -o /out/foodbox ./cmd/foodbox


FROM alpine:3.23 AS runtime

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 foodbox \
    && adduser -S -D -H -u 10001 -G foodbox foodbox \
    && mkdir -p /app/static /data \
    && chown -R foodbox:foodbox /app /data

WORKDIR /app

COPY --from=backend-build --chown=foodbox:foodbox /out/foodbox /app/foodbox
COPY --from=frontend-build --chown=foodbox:foodbox /src/front/dist/ /app/static/

ENV SERVER_PORT=8080 \
    DB_FILE_DIR=/data \
    STATIC_DIR=/app/static \
    TZ=Asia/Seoul

USER foodbox

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=4 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/foodbox"]
