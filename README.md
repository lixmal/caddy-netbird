# caddy-netbird

A [Caddy](https://caddyserver.com) plugin that embeds a [NetBird](https://netbird.io) client, allowing Caddy to proxy traffic through a NetBird network. Supports HTTP reverse proxying (Layer 7) and raw TCP/UDP proxying (Layer 4) via [caddy-l4](https://github.com/mholt/caddy-l4).

## Prerequisites

This plugin only handles the Caddy side. It joins the NetBird network as a peer using the provided setup key and dials upstream addresses through the tunnel. **All NetBird management configuration must be done beforehand:**

- The **setup key** must be created in the NetBird management dashboard.
- **Access control policies** must allow traffic from the Caddy peer to the upstream peers/services.
- **Networks (routes)** must be configured if the upstream is behind a routed network (e.g. `192.168.1.0/24`).
- The upstream peers must be online and reachable within the NetBird network.

This plugin does not create, modify, or manage any NetBird resources. It is a consumer of the network, not an administrator.

## Build

```bash
xcaddy build --with github.com/lixmal/caddy-netbird
```

With Layer 4 support (TCP/UDP proxying):

```bash
xcaddy build --with github.com/lixmal/caddy-netbird --with github.com/mholt/caddy-l4
```

Or build from source:

```bash
go build -o ./cmd/caddy/caddy ./cmd/caddy
```

## Container

```bash
podman run -d \
  --cap-add NET_BIND_SERVICE \
  -v /path/to/Caddyfile:/config/Caddyfile:ro \
  -p 80:80 -p 443:443 \
  ghcr.io/lixmal/caddy-netbird:latest
```

The image runs as a non-root user. `NET_BIND_SERVICE` is required to bind ports 80 and 443.

## Caddyfile

Minimal example, exposing a NetBird peer to the public internet:

```caddyfile
{
    netbird {
        management_url https://api.netbird.io:443
        setup_key {env.NB_SETUP_KEY}

        node ingress {
            hostname caddy-ingress
        }
    }
}

app.example.com {
    reverse_proxy backend.netbird.cloud:8080 {
        transport netbird ingress
    }
}
```

Multiple backends with load balancing:

```caddyfile
api.example.com {
    reverse_proxy app1.netbird.cloud:3000 app2.netbird.cloud:3000 {
        transport netbird ingress
        lb_policy round_robin
        health_uri /healthz
        health_interval 10s
    }
}
```

Upstream TLS, for example a DNS route through NetBird to an internal HTTPS service:

```caddyfile
secure.example.com {
    reverse_proxy https://vault.internal:443 {
        transport netbird ingress {
            tls_server_name vault.internal
        }
    }
}
```

### Layer 4 (TCP/UDP)

Requires [caddy-l4](https://github.com/mholt/caddy-l4). The `layer4` block goes inside the global options.

Proxy SSH through NetBird:

```caddyfile
{
    netbird {
        management_url https://api.netbird.io:443
        setup_key {env.NB_SETUP_KEY}

        node ingress {
            hostname caddy-ingress
        }
    }

    layer4 {
        :2222 {
            route {
                netbird backend.netbird.cloud:22 ingress
            }
        }
    }
}
```

Proxy DNS (UDP) through NetBird:

```caddyfile
{
    layer4 {
        udp/:5353 {
            route {
                netbird dns-server.netbird.cloud:53 ingress
            }
        }
    }
}
```

The network protocol (TCP or UDP) is automatically detected from the listener address. The `idle_timeout` option controls how long idle UDP associations are kept (default: 30s):

```caddyfile
{
    layer4 {
        udp/:5353 {
            idle_timeout 5m
            route {
                netbird dns-server.netbird.cloud:53 ingress
            }
        }
    }
}
```

SNI routing with TLS passthrough (no TLS termination on Caddy, upstream handles TLS):

```caddyfile
{
    layer4 {
        :443 {
            @app tls sni app.example.com
            route @app {
                netbird backend.netbird.cloud:443 ingress
            }

            @api tls sni api.example.com
            route @api {
                netbird api-backend.netbird.cloud:443 ingress
            }
        }
    }
}
```

Mixed HTTP and L4 in a single config:

```caddyfile
{
    netbird {
        management_url https://api.netbird.io:443
        setup_key {env.NB_SETUP_KEY}

        node ingress {
            hostname caddy-ingress
        }
    }

    layer4 {
        :2222 {
            route {
                netbird backend.netbird.cloud:22 ingress
            }
        }
        udp/:5353 {
            route {
                netbird backend.netbird.cloud:53 ingress
            }
        }
    }
}

app.example.com {
    reverse_proxy backend.netbird.cloud:8080 {
        transport netbird ingress
    }
}
```

## Architecture

The plugin registers three Caddy modules:

| Module | Caddy ID | Purpose |
|--------|----------|---------|
| `App` | `netbird` | Manages NetBird client lifecycle, config, and usage pool |
| `Transport` | `http.reverse_proxy.transport.netbird` | Dials HTTP upstreams through the NetBird network |
| `Handler` | `layer4.handlers.netbird` | Proxies raw TCP/UDP through the NetBird network (requires caddy-l4) |

The embedded NetBird client (`embed.Client`) runs entirely in userspace without requiring a TUN device or root privileges. Upstream traffic is dialed through the tunnel while Caddy handles TLS termination, load balancing, health checks, retries, and all other reverse proxy features.

### Client sharing

Multiple sites can share the same NetBird client by referencing the same node name. Clients are ref-counted via `caddy.UsagePool` and survive config reloads without reconnecting.

### Global options

| Option | Description |
|--------|-------------|
| `management_url` | Default management server URL |
| `setup_key` | Default setup key for authentication |
| `log_level` | NetBird client log level (default: `info`) |

### Node options

| Option | Description |
|--------|-------------|
| `management_url` | Override app-level management URL |
| `setup_key` | Override app-level setup key |
| `hostname` | Device name in the NetBird network (default: `caddy-<node>`) |
| `pre_shared_key` | Pre-shared key for the network interface |
| `wireguard_port` | Port for the network interface (default: 51820 via NetBird) |

> **Note on `wireguard_port`:** For reliable peer-to-peer connectivity, the configured port (or the default random port) should be exposed via port forwarding on the host's firewall/NAT. Without it, connections may fall back to relayed traffic which adds latency.

### Multiple nodes

Each node creates a separate NetBird peer identity. This is useful when connecting to different networks or management servers from a single Caddy instance.

**Different management servers.** No shared defaults needed, each node specifies its own management URL and setup key:

```caddyfile
{
    netbird {
        node corp {
            management_url https://netbird.corp.example.com:443
            setup_key {env.NB_CORP_KEY}
            hostname caddy-corp
            wireguard_port 0
        }

        node staging {
            management_url https://netbird.staging.example.com:443
            setup_key {env.NB_STAGING_KEY}
            hostname caddy-staging
            wireguard_port 0
        }
    }
}

corp-app.example.com {
    reverse_proxy backend.corp.internal:8080 {
        transport netbird corp
    }
}

staging-app.example.com {
    reverse_proxy backend.staging.internal:8080 {
        transport netbird staging
    }
}
```

**Same management, different setup keys.** Useful for separate peer identities with different access policies on the same network:

```caddyfile
{
    netbird {
        management_url https://api.netbird.io:443

        node web {
            setup_key {env.NB_WEB_KEY}
            hostname caddy-web
            wireguard_port 0
        }

        node api {
            setup_key {env.NB_API_KEY}
            hostname caddy-api
            wireguard_port 0
        }
    }
}

web.example.com {
    reverse_proxy web-backend.netbird.cloud:8080 {
        transport netbird web
    }
}

api.example.com {
    reverse_proxy api-backend.netbird.cloud:3000 {
        transport netbird api
    }
}
```

> **Note:** Each node binds its own network interface port. When running multiple nodes, set distinct `wireguard_port` values to avoid conflicts.

### Upstream TLS

The NetBird network encryption and upstream TLS are independent concerns. The upstream behind the tunnel may be a plain HTTP service on a peer, or it could be an HTTPS endpoint reached via a NetBird route to an external network.

TLS to the upstream is automatically enabled when using `https://` upstream addresses. You can also configure it explicitly in the transport block:

| Option | Description |
|--------|-------------|
| `tls` | Enable TLS to upstream with default settings |
| `tls_insecure_skip_verify` | Skip TLS certificate verification (testing only) |
| `tls_server_name` | Override the server name for TLS verification |
