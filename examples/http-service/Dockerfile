FROM golang:1.23 AS builder

WORKDIR /workspace

COPY . .

WORKDIR /workspace/examples/http-service
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -o /workspace/bin/http-service ./main.go

FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /workspace/bin/http-service /app/http-service
COPY audio_*.pcm /app/

EXPOSE 8080

ENTRYPOINT ["/app/http-service"]
