# Build the manager binary
# FROM golang:1.30 AS builder
FROM golang:1.22 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY README.md README.md
# COPY go.mod go.mod
# COPY go.sum go.sum
# # cache deps before building and copying source so that we don't need to re-download as much
# # and so that source changes don't invalidate our downloaded layer
# # ENV GODEBUG=http2debug=2
# # ENV GOCACHE=/tmp/go-cache
# # ENV GO_RETRY_CONN_TIMEOUT=5s
# # ENV GOPROXY=direct
# # RUN go mod download
# RUN go mod tidy

# # Copy the go source
# COPY cmd/main.go cmd/main.go
# COPY api/ api/
# COPY internal/ internal/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
# RUN GOOS=${TARGETARCH}${TARGETOS:-linux} GOARCH= go build -a -o manager cmd/main.go
COPY manager manager
# RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
