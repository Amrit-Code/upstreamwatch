# Build from the repository root: docker build -f deploy/Dockerfile .
# Both base images are pinned by their multi-platform index digest so a rebuild
# cannot silently pull a different image; the tag is kept for readability.

FROM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/upw ./cmd/upw

# Alpine rather than distroless: internal/gitsrc runs git at runtime and the
# compose health check uses busybox wget.
FROM alpine:3.22@sha256:49340b346798875b5592e5d420241f2f9cfba5f47e62ebb1446a1b34734e898f

RUN apk add --no-cache git ca-certificates tzdata \
	&& addgroup -S -g 10001 upw \
	&& adduser -S -D -H -u 10001 -G upw -h /var/lib/upstreamwatch -s /sbin/nologin upw \
	&& install -d -o upw -g upw -m 0700 /var/lib/upstreamwatch \
	&& install -d /etc/upstreamwatch

COPY --from=builder /out/upw /usr/local/bin/upw

USER 10001:10001
WORKDIR /var/lib/upstreamwatch
EXPOSE 8080

ENTRYPOINT ["upw"]
CMD ["serve", "-config", "/etc/upstreamwatch/upw.yaml"]
