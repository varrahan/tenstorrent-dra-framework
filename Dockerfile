# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS build

WORKDIR /src
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=1970-01-01T00:00:00Z
ARG SOURCE_DATE_EPOCH=0

COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY src ./src
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -mod=readonly -trimpath -buildvcs=false \
    -ldflags="-s -w -buildid= -X main.version=${VERSION} -X main.commit=${VCS_REF} -X main.buildDate=${BUILD_DATE}" \
    -o /out/tt-dra-driver ./src/cmd/tt-dra-driver && \
    touch --date="@${SOURCE_DATE_EPOCH}" /out/tt-dra-driver

FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7

ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=1970-01-01T00:00:00Z
LABEL org.opencontainers.image.title="Tenstorrent Kubernetes DRA driver" \
      org.opencontainers.image.description="Topology-aware Tenstorrent accelerator allocation for Kubernetes" \
      org.opencontainers.image.source="https://github.com/varrahan/tenstorrent-dra-framework" \
      org.opencontainers.image.url="https://github.com/varrahan/tenstorrent-dra-framework" \
      org.opencontainers.image.documentation="https://github.com/varrahan/tenstorrent-dra-framework/tree/main/docs" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"

COPY --from=build --chown=65532:65532 /out/tt-dra-driver /tt-dra-driver-bin
USER 65532:65532
ENTRYPOINT ["/tt-dra-driver-bin"]
