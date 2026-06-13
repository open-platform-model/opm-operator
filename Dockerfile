# Build the manager binary
# Pin the builder to the *build* platform so it always runs natively (e.g. amd64 on
# CI runners) and cross-compiles to the target arch via GOARCH. The binary is pure
# Go (CGO_ENABLED=0), so this avoids running the toolchain under QEMU emulation for
# non-native targets — the dominant cost of multi-arch builds.
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.sum ./
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the Go source (relies on .dockerignore to filter)
COPY . .

# Build. GOARCH=${TARGETARCH} cross-compiles to the requested target platform.
# BuildKit cache mounts persist the module + build caches across builds so source
# changes don't force a from-scratch recompile of dependencies and the stdlib.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -o manager cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
