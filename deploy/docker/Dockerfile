# syntax=docker/dockerfile:1.7
# Multi-arch build: the Go toolchain runs natively on the build host and
# cross-compiles the cgo parts (libusb, librtlsdr, multimon-ng) with clang
# through tonistiigi/xx, producing a fully static musl binary per platform.
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.10.0 AS xx

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
COPY --from=xx / /
RUN apk add --no-cache clang lld
WORKDIR /src
# Modules are platform independent: downloading them before TARGETPLATFORM
# comes into scope shares one layer (and one download) across all platforms.
COPY go.mod go.sum ./
RUN go mod download
ARG TARGETPLATFORM
RUN xx-apk add --no-cache musl-dev gcc linux-headers
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ENV CGO_ENABLED=1
# -buildvcs=false: the build context has no git binary; the commit comes in via COMMIT.
RUN xx-go build -trimpath -buildvcs=false \
      -ldflags "-s -w -linkmode external -extldflags -static -X github.com/gopager/pagewire/internal/buildinfo.Version=${VERSION} -X github.com/gopager/pagewire/internal/buildinfo.commit=${COMMIT}" \
      -o /out/pagewire ./cmd/pagewire \
 && xx-verify --static /out/pagewire

# Bare binaries only: docker buildx build --target artifacts --output type=local,dest=dist .
FROM scratch AS artifacts
COPY --from=build /out/pagewire /pagewire

# Runtime image. The process needs access to the USB device node
# (--device /dev/bus/usb); file permissions on the host decide whether a
# non-root user may open it, so the image does not force a user.
FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata \
 && mkdir -p /etc/pagewire /var/lib/pagewire
COPY --from=build /out/pagewire /usr/local/bin/pagewire
VOLUME ["/var/lib/pagewire"]
EXPOSE 9464
LABEL org.opencontainers.image.source="https://github.com/gopager/pagewire" \
      org.opencontainers.image.licenses="GPL-3.0-or-later" \
      org.opencontainers.image.description="SDR receiver, decoder and delivery gateway for pager and emergency-service radio protocols"
ENTRYPOINT ["/usr/local/bin/pagewire"]
CMD ["run", "--config", "/etc/pagewire/pagewire.yaml"]
