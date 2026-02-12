// Package transport provides a Caddy reverse proxy transport that dials
// upstream servers through a NetBird network tunnel.
package transport

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"go.uber.org/zap"

	"github.com/lixmal/caddy-netbird/app"
)

func init() {
	caddy.RegisterModule(Transport{})
}

// Transport is a reverse proxy transport that dials upstream servers through
// a NetBird network tunnel.
type Transport struct {
	// Node is the name of the NetBird node to use for dialing.
	// Must match a node defined in the top-level netbird app config.
	// Defaults to "default" if empty.
	Node string `json:"node,omitempty"`

	// TLS configures TLS to the upstream. Setting this to an empty struct
	// enables TLS with reasonable defaults. This is independent of the
	// NetBird network encryption. The upstream behind NetBird may require
	// its own TLS (e.g. when routing through a NetBird network to an
	// external HTTPS service).
	TLS *reverseproxy.TLSConfig `json:"tls,omitempty"`

	nbApp  *app.App
	mc     *app.ManagedClient
	rt     http.RoundTripper
	logger *zap.Logger
	ctx    caddy.Context
}

// CaddyModule returns the Caddy module information.
func (Transport) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.reverse_proxy.transport.netbird",
		New: func() caddy.Module { return new(Transport) },
	}
}

// Provision initializes the transport by obtaining a ref-counted NetBird client
// from the app pool and starting it if necessary.
func (t *Transport) Provision(ctx caddy.Context) error {
	t.logger = ctx.Logger()
	t.ctx = ctx

	if t.Node == "" {
		t.Node = "default"
	}

	appModule, err := ctx.App("netbird")
	if err != nil {
		return fmt.Errorf("load netbird app module: %w", err)
	}
	t.nbApp = appModule.(*app.App)

	t.mc, err = t.nbApp.GetClient(t.Node)
	if err != nil {
		return fmt.Errorf("get netbird client %q: %w", t.Node, err)
	}

	if err := t.mc.Start(ctx); err != nil {
		return fmt.Errorf("start netbird client %q: %w", t.Node, err)
	}

	ht := &http.Transport{
		DialContext: t.mc.Client().DialContext,
	}

	if t.TLS != nil {
		tlsConfig, err := t.TLS.MakeTLSClientConfig(ctx)
		if err != nil {
			return fmt.Errorf("configure upstream TLS: %w", err)
		}
		ht.TLSClientConfig = tlsConfig
	}

	t.rt = ht

	t.logger.Info("netbird transport provisioned",
		zap.String("node", t.Node),
		zap.Bool("tls", t.TLS != nil),
	)
	return nil
}

// RoundTrip sends the request through the NetBird network tunnel.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "" {
		req.URL.Scheme = "http"
		if t.TLSEnabled() {
			req.URL.Scheme = "https"
		}
	}
	return t.rt.RoundTrip(req)
}

// TLSEnabled returns true if upstream TLS is configured.
func (t *Transport) TLSEnabled() bool {
	return t.TLS != nil
}

// EnableTLS enables TLS to the upstream. Called by Caddy when the upstream
// scheme is https://. This is separate from the NetBird network encryption;
// the upstream behind the tunnel may itself require TLS.
func (t *Transport) EnableTLS(base *reverseproxy.TLSConfig) error {
	t.TLS = base
	return nil
}

// TLSClientConfig returns the TLS config for upstream connections, or nil
// if TLS is not enabled.
func (t *Transport) TLSClientConfig() *tls.Config {
	if t.TLS == nil {
		return nil
	}
	cfg, err := t.TLS.MakeTLSClientConfig(t.ctx)
	if err != nil {
		t.logger.Debug("build TLS client config", zap.Error(err))
		return nil
	}
	return cfg
}

// Cleanup releases the client reference back to the pool and closes idle connections.
func (t *Transport) Cleanup() error {
	if ht, ok := t.rt.(*http.Transport); ok {
		ht.CloseIdleConnections()
	}
	if t.nbApp != nil {
		return t.nbApp.ReleaseClient(t.Node)
	}
	return nil
}

// UnmarshalCaddyfile parses the transport subdirective within a reverse_proxy block.
//
//	reverse_proxy <upstream> {
//	    transport netbird [<node>] {
//	        tls
//	        tls_insecure_skip_verify
//	        tls_server_name <name>
//	    }
//	}
//
// TLS to the upstream is also automatically enabled when using https:// upstream
// addresses, independent of these options.
func (t *Transport) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume "netbird"

	if d.NextArg() {
		t.Node = d.Val()
	}

	for d.NextBlock(0) {
		switch d.Val() {
		case "tls":
			if t.TLS == nil {
				t.TLS = new(reverseproxy.TLSConfig)
			}

		case "tls_insecure_skip_verify":
			if t.TLS == nil {
				t.TLS = new(reverseproxy.TLSConfig)
			}
			t.TLS.InsecureSkipVerify = true

		case "tls_server_name":
			if !d.NextArg() {
				return d.ArgErr()
			}
			if t.TLS == nil {
				t.TLS = new(reverseproxy.TLSConfig)
			}
			t.TLS.ServerName = d.Val()

		default:
			return d.Errf("unrecognized netbird transport option: %s", d.Val())
		}
	}

	return nil
}

var (
	_ http.RoundTripper         = (*Transport)(nil)
	_ caddy.Provisioner         = (*Transport)(nil)
	_ caddy.CleanerUpper        = (*Transport)(nil)
	_ reverseproxy.TLSTransport = (*Transport)(nil)
	_ caddyfile.Unmarshaler     = (*Transport)(nil)
)
