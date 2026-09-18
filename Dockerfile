# syntax=docker/dockerfile:1

# ---- build ------------------------------------------------------------------
FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
# PocketBase uses a pure Go SQLite driver, so the binary is fully static.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w \
        -X github.com/boyulkim/api-template/internal/version.Version=${VERSION} \
        -X github.com/boyulkim/api-template/internal/version.Commit=${COMMIT}" \
      -o /out/server ./cmd/server

# Data dir owned by the distroless "nonroot" user (65532).
RUN mkdir -p /out/pb_data

# ---- runtime ----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /app/server
COPY --from=build --chown=65532:65532 /out/pb_data /pb_data

ENV APP_ENV=production \
    HTTP_ADDR=0.0.0.0:8090 \
    PB_DATA_DIR=/pb_data

VOLUME ["/pb_data"]
EXPOSE 8090
USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/app/server", "healthcheck"]

ENTRYPOINT ["/app/server"]
