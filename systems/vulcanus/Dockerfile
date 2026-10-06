FROM golang:1.25-bookworm AS builder

WORKDIR /build

COPY systems/vulcanus/go.mod systems/vulcanus/go.sum .
COPY systems/vulcanus/internal/ ./internal/
COPY systems/vulcanus/main.go ./main.go

RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=1 go build -o /vulcanus ./

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates openssl curl && rm -rf /var/lib/apt/lists/*
COPY --from=builder /vulcanus /usr/local/bin/vulcanus
WORKDIR /data
ENTRYPOINT ["vulcanus"]
