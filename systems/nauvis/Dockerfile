FROM golang:1.25-bookworm AS builder

WORKDIR /build

# croid source required by the replace directive (../croid) in systems/nauvis/go.mod
COPY systems/croid/go.mod systems/croid/go.sum ./croid/
COPY systems/croid/client/ ./croid/client/
COPY systems/nauvis/go.mod systems/nauvis/go.sum ./nauvis/
COPY systems/nauvis/internal/ ./nauvis/internal/
COPY systems/nauvis/main.go ./nauvis/main.go

RUN --mount=type=cache,target=/go/pkg/mod \
    cd /build/nauvis && CGO_ENABLED=1 go build -o /nauvis .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /nauvis /usr/local/bin/nauvis
VOLUME ["/data"]
WORKDIR /data
ENTRYPOINT ["nauvis"]
