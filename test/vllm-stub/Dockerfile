FROM --platform=$BUILDPLATFORM golang:1.26.5 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY test/vllm-stub/ test/vllm-stub/

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o vllm-stub ./test/vllm-stub

# The generated preStop drain hook requires /bin/sh.
FROM alpine:3.24
WORKDIR /
COPY --from=builder /workspace/vllm-stub .
USER 65532:65532

ENTRYPOINT ["/vllm-stub"]
