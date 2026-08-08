# syntax=docker/dockerfile:1.7
FROM golang:1.25.8-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/admin ./cmd/admin

FROM gcr.io/distroless/static-debian12:nonroot AS gateway
COPY --from=build /out/gateway /gateway
ENTRYPOINT ["/gateway"]

# Local-only analyzer profile. go/packages needs the Go toolchain at runtime to
# inspect an allowlisted bind-mounted repository. Production should isolate
# this capability in a sandboxed analysis worker.
FROM golang:1.25.8-bookworm AS gateway-analyzer
RUN git config --system --add safe.directory '*'
ENV GOCACHE=/tmp/go-build \
    GOMODCACHE=/tmp/go-mod \
    GOTOOLCHAIN=local
COPY --from=build /out/gateway /gateway
USER 65532:65532
WORKDIR /workspace
ENTRYPOINT ["/gateway"]

FROM gcr.io/distroless/static-debian12:nonroot AS worker
COPY --from=build /out/worker /worker
ENTRYPOINT ["/worker"]

FROM gcr.io/distroless/static-debian12:nonroot AS admin
COPY --from=build /out/admin /admin
ENTRYPOINT ["/admin"]
