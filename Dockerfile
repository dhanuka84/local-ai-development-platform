# syntax=docker/dockerfile:1.7
FROM golang:1.26.0-bookworm AS build
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

# Compiler-backed SCIP indexers for TypeScript, JavaScript, and Python. The
# package versions and multi-architecture Node image are pinned for repeatable
# local builds.
FROM node:20.19.5-bookworm-slim@sha256:9e70124bd00f47dd023e349cd587132ae61892acc0e47ed641416c3e18f401c3 AS scip-node
RUN npm install --global --ignore-scripts \
    @sourcegraph/scip-typescript@0.4.0 \
    @sourcegraph/scip-python@0.6.6

FROM gradle:9.1.0-jdk25@sha256:d2f954187670397de6dd42c5c3a9d4535409b590059c6d248ff2a59ba67cecc3 AS gradle-tools

# Local-only analyzer profile. Repositories are mounted read-only and copied to
# a disposable directory before an indexer or build tool runs.
FROM maven:3.9.11-eclipse-temurin-25@sha256:407c4423cec0cf2981055bc2c6c0dc211d9605b6669279b95997f2d1c7e91e2c AS gateway-analyzer
RUN apt-get update && apt-get install --yes --no-install-recommends git python3 python3-pip && \
    rm -rf /var/lib/apt/lists/*
COPY deploy/analyzer/scip-java-runtime.pom.xml /tmp/scip-java-runtime.pom.xml
RUN mvn --batch-mode --file /tmp/scip-java-runtime.pom.xml dependency:copy-dependencies -DoutputDirectory=/opt/scip-java && \
    rm -rf /root/.m2 /tmp/scip-java-runtime.pom.xml
COPY --from=build /usr/local/go /usr/local/go
COPY --from=scip-node /usr/local/ /usr/local/
COPY --from=gradle-tools /opt/gradle /opt/gradle
RUN git config --system --add safe.directory '*'
ENV PATH=/usr/local/go/bin:/usr/local/bin:/opt/gradle/bin:/usr/share/maven/bin:/opt/java/openjdk/bin:/usr/bin:/bin \
    HOME=/tmp/analyzer-home \
    GOCACHE=/tmp/go-build \
    GOMODCACHE=/tmp/go-mod \
    GOTOOLCHAIN=local \
    NPM_CONFIG_CACHE=/tmp/npm-cache \
    NODE_OPTIONS=--max-old-space-size=8192
COPY deploy/analyzer/hybrid-index-jvm /usr/local/bin/hybrid-index-jvm
COPY deploy/analyzer/hybrid-index-typescript /usr/local/bin/hybrid-index-typescript
COPY deploy/analyzer/hybrid-index-python /usr/local/bin/hybrid-index-python
COPY deploy/analyzer/scip-java /usr/local/bin/scip-java
RUN chmod 0755 /usr/local/bin/scip-java /usr/local/bin/hybrid-index-jvm /usr/local/bin/hybrid-index-typescript /usr/local/bin/hybrid-index-python
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
