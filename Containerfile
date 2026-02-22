FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /caddy ./cmd/caddy

FROM alpine:latest

RUN adduser -D -h /var/lib/caddy -u 1000 caddy && \
    mkdir -p /config /data && chown caddy:caddy /data

COPY --from=builder /caddy /usr/bin/caddy

USER caddy:caddy
ENV HOME=/var/lib/caddy

EXPOSE 80 443

ENTRYPOINT ["caddy"]
CMD ["run", "--config", "/config/Caddyfile", "--adapter", "caddyfile"]
