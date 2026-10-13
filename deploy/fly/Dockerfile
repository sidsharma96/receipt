# Build from the repository root. Confirm image digests before first deployment.
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/receipt ./cmd/receipt

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates gosu util-linux \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 receipt \
    && useradd --uid 10001 --gid receipt --no-create-home receipt
WORKDIR /app
COPY --from=build /out/receipt /usr/local/bin/receipt
COPY --chmod=755 deploy/fly/entrypoint.sh /usr/local/bin/receipt-entrypoint
EXPOSE 7777
ENTRYPOINT ["/usr/local/bin/receipt-entrypoint"]
CMD ["serve", "--postgres", "--credentials", "--listen", "0.0.0.0:7777", "--cache-dir", "/data/cache", "--max-inflight", "2", "--request-log"]
