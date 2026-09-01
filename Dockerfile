# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.6
ARG NODE_VERSION=22
ARG ALPINE_VERSION=3.20

FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w \
      -X github.com/ArloB/tickets/internal/buildinfo.Version=${VERSION} \
      -X github.com/ArloB/tickets/internal/buildinfo.Commit=${COMMIT} \
      -X github.com/ArloB/tickets/internal/buildinfo.Date=${DATE}" \
    -o /out/tickets ./cmd/tickets

FROM alpine:${ALPINE_VERSION} AS runtime
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S tickets -g 10001 && \
    adduser -S tickets -G tickets -u 10001 && \
    mkdir -p /data && \
    chown -R tickets:tickets /data
COPY --from=build /out/tickets /usr/local/bin/tickets
USER tickets
ENV TICKETS_DATA_DIR=/data \
    TICKETS_HOST=0.0.0.0 \
    TICKETS_PORT=8080
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/tickets"]
CMD ["server"]
