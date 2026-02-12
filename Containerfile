FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -o /caddy ./cmd/caddy

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /caddy /usr/bin/caddy

RUN mkdir -p /var/lib/caddy/netbird /config /data

EXPOSE 80 443

ENTRYPOINT ["caddy"]
CMD ["run", "--config", "/config/Caddyfile", "--adapter", "caddyfile"]
