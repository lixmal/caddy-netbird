FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /caddy ./cmd/caddy

RUN echo "caddy:x:1000:1000:caddy:/var/lib/caddy:/sbin/nologin" > /tmp/passwd && \
    echo "caddy:x:1000:caddy" > /tmp/group && \
    mkdir -p /tmp/var/lib/caddy /tmp/config /tmp/data

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /caddy /usr/bin/caddy
COPY --from=builder /tmp/passwd /etc/passwd
COPY --from=builder /tmp/group /etc/group
COPY --from=builder --chown=1000:1000 /tmp/var/lib/caddy /var/lib/caddy
COPY --from=builder /tmp/config /config
COPY --from=builder --chown=1000:1000 /tmp/data /data

USER caddy:caddy
ENV HOME=/var/lib/caddy

EXPOSE 80 443

ENTRYPOINT ["caddy"]
CMD ["run", "--config", "/config/Caddyfile", "--adapter", "caddyfile"]
