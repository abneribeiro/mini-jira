# syntax=docker/dockerfile:1

# ---- build ----------------------------------------------------------------
FROM golang:1.27-alpine AS build

WORKDIR /src

# Dependencies first: this layer is cached until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a static binary, which is what lets the final stage
# be scratch. -trimpath and -ldflags drop build paths and debug info.
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/migrate ./cmd/migrate

# ---- runtime --------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /app

COPY --from=build /out/api /app/api
COPY --from=build /out/migrate /app/migrate

# distroless:nonroot runs as uid 65532; nothing here needs to write to disk.
USER nonroot:nonroot

EXPOSE 8080 9091

ENTRYPOINT ["/app/api"]
