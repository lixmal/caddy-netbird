// Package l4handler provides a Caddy layer4 handler that proxies raw
// TCP/UDP connections through a NetBird network tunnel.
package l4handler

import (
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/mholt/caddy-l4/layer4"
	"go.uber.org/zap"

	"github.com/lixmal/caddy-netbird/app"
)

func init() {
	caddy.RegisterModule(&Handler{})
}

// Handler is a layer4 handler that proxies connections through a NetBird
// network tunnel to the configured upstream.
type Handler struct {
	// Upstream is the host:port to dial via the NetBird network.
	Upstream string `json:"upstream"`
	// Node is the name of the NetBird node to use for dialing.
	// Must match a node defined in the top-level netbird app config.
	// Defaults to "default" if empty.
	Node string `json:"node,omitempty"`

	nbApp  *app.App
	mc     *app.ManagedClient
	logger *zap.Logger
}

// CaddyModule returns the Caddy module information.
func (*Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "layer4.handlers.netbird",
		New: func() caddy.Module { return new(Handler) },
	}
}

// Provision initializes the handler by obtaining a ref-counted NetBird client
// from the app pool and starting it.
func (h *Handler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger()

	if h.Node == "" {
		h.Node = "default"
	}

	appModule, err := ctx.App("netbird")
	if err != nil {
		return fmt.Errorf("load netbird app module: %w", err)
	}
	h.nbApp = appModule.(*app.App)

	h.mc, err = h.nbApp.GetClient(h.Node)
	if err != nil {
		return fmt.Errorf("get netbird client %q: %w", h.Node, err)
	}

	if err := h.mc.Start(ctx); err != nil {
		return fmt.Errorf("start netbird client %q: %w", h.Node, err)
	}

	h.logger.Info("netbird l4 handler provisioned",
		zap.String("node", h.Node),
		zap.String("upstream", h.Upstream),
	)
	return nil
}

// Handle dials the upstream through the NetBird tunnel and proxies
// the connection bidirectionally.
func (h *Handler) Handle(cx *layer4.Connection, _ layer4.Handler) error {
	network := networkFromAddr(cx.LocalAddr())

	up, err := h.mc.Client().DialContext(cx.Context, network, h.Upstream)
	if err != nil {
		return fmt.Errorf("dial %s upstream %s via netbird: %w", network, h.Upstream, err)
	}
	defer up.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		if _, err := io.Copy(cx, up); err != nil {
			h.logger.Debug("copy upstream to downstream", zap.Error(err))
		}
		if cw, ok := cx.Conn.(closeWriter); ok {
			if err := cw.CloseWrite(); err != nil {
				h.logger.Debug("half-close downstream write side", zap.Error(err))
			}
		}
	}()

	if _, err := io.Copy(up, cx); err != nil {
		h.logger.Debug("copy downstream to upstream", zap.Error(err))
	}
	if cw, ok := up.(closeWriter); ok {
		if err := cw.CloseWrite(); err != nil {
			h.logger.Debug("half-close upstream write side", zap.Error(err))
		}
	} else {
		if err := up.Close(); err != nil {
			h.logger.Debug("close upstream", zap.Error(err))
		}
	}

	wg.Wait()
	return nil
}

// networkFromAddr returns "udp" for UDP addresses and "tcp" for everything else.
func networkFromAddr(addr net.Addr) string {
	if addr == nil {
		return "tcp"
	}
	switch addr.Network() {
	case "udp", "udp4", "udp6":
		return "udp"
	default:
		return "tcp"
	}
}

// Cleanup releases the client reference back to the pool.
func (h *Handler) Cleanup() error {
	if h.nbApp != nil {
		return h.nbApp.ReleaseClient(h.Node)
	}
	return nil
}

// UnmarshalCaddyfile parses the handler directive within a layer4 route block.
//
//	layer4 {
//	    :2222 {
//	        route {
//	            netbird <upstream_host:port> [<node_name>]
//	        }
//	    }
//	}
func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume "netbird"

	if !d.NextArg() {
		return d.ArgErr()
	}
	h.Upstream = d.Val()

	if d.NextArg() {
		h.Node = d.Val()
	}

	if d.NextBlock(0) {
		return d.Errf("unexpected block in netbird l4 handler")
	}

	return nil
}

type closeWriter interface {
	CloseWrite() error
}

var (
	_ layer4.NextHandler    = (*Handler)(nil)
	_ caddy.Provisioner     = (*Handler)(nil)
	_ caddy.CleanerUpper    = (*Handler)(nil)
	_ caddyfile.Unmarshaler = (*Handler)(nil)
)
