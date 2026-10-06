FROM golang:1.25-bookworm AS builder

WORKDIR /build

COPY systems/croid/go.mod systems/croid/go.sum .
COPY systems/croid/internal/ ./internal/
COPY systems/croid/main.go ./main.go

RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=1 go build -o /croid .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates openssl curl && rm -rf /var/lib/apt/lists/*
COPY --from=builder /croid /usr/local/bin/croid
WORKDIR /data
ENTRYPOINT ["croid"]
