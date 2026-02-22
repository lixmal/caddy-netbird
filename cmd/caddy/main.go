// Build a custom Caddy binary with the NetBird plugin.
// This is equivalent to running: xcaddy build --with github.com/lixmal/caddy-netbird
package main

import (
	caddycmd "github.com/caddyserver/caddy/v2/cmd"

	_ "github.com/caddyserver/caddy/v2/modules/standard"
	_ "github.com/lixmal/caddy-netbird/app"
	_ "github.com/lixmal/caddy-netbird/l4handler"
	_ "github.com/lixmal/caddy-netbird/transport"
	_ "github.com/mholt/caddy-l4"
)

func main() {
	caddycmd.Main()
}
