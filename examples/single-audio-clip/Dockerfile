FROM golang:1.23 AS builder

WORKDIR /workspace

COPY . .

WORKDIR /workspace/examples/single-audio-clip
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -o /workspace/bin/single-audio-clip ./main.go

FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /workspace/bin/single-audio-clip /app/single-audio-clip
COPY audio.pcm /app/audio.pcm

ENTRYPOINT ["/app/single-audio-clip"]

