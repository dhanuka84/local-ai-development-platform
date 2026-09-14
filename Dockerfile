# syntax=docker/dockerfile:1.7
FROM golang:1.27.1-bookworm AS go-toolchain

# Reproducible local checks, including the Python setup tests. No source or
# runtime credentials are baked into this target; make mounts the workspace.
FROM go-toolchain AS buildcheck
RUN apt-get update && apt-get install --yes --no-install-recommends python3 && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /src

# Build dependencies before attaching the acceptance runner to its private test
# network. Test execution needs no public network or model provider.
FROM buildcheck AS agent-ready-check
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV GOFLAGS=-buildvcs=false
RUN mkdir -p /opt/agent-ready-bin && go build -o /opt/agent-ready-bin/ ./cmd/gateway ./cmd/worker ./cmd/admin ./cmd/agent-ready-pilot
CMD ["python3", "scripts/agent_ready_e2e.py", "run", "--output", "/evidence"]

FROM go-toolchain AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/admin ./cmd/admin && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/sdlc-verifier ./cmd/sdlc-verifier

# The evaluator worker starts this image with no network, no capabilities, a
# read-only root and only a credential-free snapshot mounted read-only.
FROM buildcheck AS sdlc-evaluator
COPY --from=build /out/sdlc-verifier /sdlc-verifier
USER 65532:65532
WORKDIR /tmp
ENTRYPOINT ["/sdlc-verifier"]

FROM gcr.io/distroless/static-debian12:nonroot AS gateway
COPY --from=build /out/gateway /gateway
ENTRYPOINT ["/gateway"]

# Compiler-backed SCIP indexers for TypeScript, JavaScript, and Python. The
# package versions and multi-architecture Node image are pinned for repeatable
# local builds.
FROM node:26.8-bookworm-slim@sha256:cd9f682fa2885cd1056e830424764158570061c59736a1da836bc3d73df095ae AS scip-node
RUN npm install --global --ignore-scripts \
    @sourcegraph/scip-typescript@0.4.0 \
    @sourcegraph/scip-python@0.6.6

FROM gradle:9.7.1-jdk25@sha256:eec96c1a66c66235f8d58d4fdb3ee3b9c52852d895001cc3183388fbf421ff42 AS gradle-tools

# Local-only analyzer profile. Repositories are mounted read-only and copied to
# a disposable directory before an indexer or build tool runs.
FROM maven:3.9.15-eclipse-temurin-26@sha256:029a8e2838ae68238ffb8be407cddbb3f07d4d839c60c6f26c619a69fd184531 AS gateway-analyzer
RUN apt-get update && apt-get install --yes --no-install-recommends git python3 python3-pip libatomic1 && \
    rm -rf /var/lib/apt/lists/*
COPY deploy/analyzer/scip-java-runtime.pom.xml /tmp/scip-java-runtime.pom.xml
RUN mvn --batch-mode --file /tmp/scip-java-runtime.pom.xml dependency:copy-dependencies -DoutputDirectory=/opt/scip-java && \
    rm -rf /root/.m2 /tmp/scip-java-runtime.pom.xml
COPY --from=build /usr/local/go /usr/local/go
COPY --from=scip-node /usr/local/ /usr/local/
COPY --from=gradle-tools /opt/gradle /opt/gradle
# The copied Node runtime needs libatomic on arm64. Exercise the actual indexer
# launchers during the image build so dependency updates cannot hide ABI gaps.
RUN node --version && scip-typescript --version && scip-python --version && java --version
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

FROM debian:bookworm-slim AS git-runtime
RUN apt-get update && apt-get install --yes --no-install-recommends git ca-certificates && apt-get clean
USER 65532:65532

# Read-only source verification without the synchronous analyzer toolchains.
FROM git-runtime AS gateway-source-verifier
COPY --from=build /out/gateway /gateway
ENTRYPOINT ["/gateway"]

FROM git-runtime AS worker
COPY --from=build /out/worker /worker
ENTRYPOINT ["/worker"]

FROM gcr.io/distroless/static-debian12:nonroot AS admin
COPY --from=build /out/admin /admin
ENTRYPOINT ["/admin"]
