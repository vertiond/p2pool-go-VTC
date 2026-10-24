FROM golang:1.25-alpine AS build

ARG VERSION=docker
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Same flags as the release workflow: static, stripped, versioned
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/CADMonkey21/p2pool-go-VTC/config.Version=${VERSION}" \
    -o /out/p2pool-go-vtc .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
    && adduser -D -u 1000 -h /home/p2pool p2pool \
    && mkdir -p /data \
    && chown p2pool:p2pool /data
# The dashboard is served from web/static next to the executable
COPY --from=build /out/p2pool-go-vtc /app/p2pool-go-vtc
COPY --from=build /src/web/static /app/web/static

# config.yaml, peer.key, sharechain.json, blocks.json and p2pool.log are read and
# written in the working directory; verthash.dat (~1.2 GB) is mounted, never baked in.
USER p2pool
WORKDIR /data
VOLUME /data
EXPOSE 9172 9348

ENTRYPOINT ["/app/p2pool-go-vtc", "-datadir", "/data"]
CMD ["-verthash", "/data/verthash.dat"]
