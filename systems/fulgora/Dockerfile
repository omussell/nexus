FROM golang:1.25-bookworm AS builder

WORKDIR /build

# croid source required by the replace directive (../croid) in systems/fulgora/go.mod
COPY systems/croid/go.mod systems/croid/go.sum ./croid/
COPY systems/croid/client/ ./croid/client/
COPY systems/fulgora/go.mod systems/fulgora/go.sum ./fulgora/
COPY systems/fulgora/internal/ ./fulgora/internal/
COPY systems/fulgora/main.go ./fulgora/main.go

RUN --mount=type=cache,target=/go/pkg/mod \
    cd /build/fulgora && CGO_ENABLED=1 go build -o /fulgora .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /fulgora /usr/local/bin/fulgora
VOLUME ["/data"]
WORKDIR /data
ENTRYPOINT ["fulgora"]
