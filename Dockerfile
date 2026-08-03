FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/tt-dra-driver ./src/cmd/tt-dra-driver

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/tt-dra-driver /tt-dra-driver
ENTRYPOINT ["/tt-dra-driver"]
