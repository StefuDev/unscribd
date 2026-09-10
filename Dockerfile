FROM golang:1.27-bookworm AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN if [ -f default.pgo ]; then \
      go build -buildvcs=false -trimpath -pgo=default.pgo -ldflags="-s -w" -o /out/unscribd-web ./cmd/unscribd-web; \
    else \
      go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/unscribd-web ./cmd/unscribd-web; \
    fi

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates woff2 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/unscribd-web /usr/local/bin/unscribd-web
COPY web/templates /app/web/templates
COPY web/static /app/web/static
WORKDIR /app
ENV PORT=10000
EXPOSE 10000
CMD ["/usr/local/bin/unscribd-web"]
