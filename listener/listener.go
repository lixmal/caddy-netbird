// Package listener registers "netbird" and "netbird-udp" network types with
// Caddy, enabling listeners bound to the NetBird virtual interface.
//
// Use the address format "netbird/<node>:<port>" (TCP) or
// "netbird-udp/<node>:<port>" (UDP) in Caddyfile listen directives. For example:
//
//	layer4 {
//	    netbird/egress:9080 {
//	        route {
//	            proxy upstream:80
//	        }
//	    }
//	    netbird-udp/egress:9053 {
//	        route {
//	            proxy upstream:53
//	        }
//	    }
//	}
package listener

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/caddyserver/caddy/v2"

	"github.com/lixmal/caddy-netbird/app"
)

func init() {
	caddy.RegisterNetwork("netbird", listenNetbirdTCP)
	caddy.RegisterNetwork("netbird-udp", listenNetbirdUDP)
}

// getClientAndPort validates the listener arguments, acquires a managed client,
// and starts it. On success the caller owns a ref that must be released via
// ReleaseClient.
func getClientAndPort(ctx context.Context, host, portRange string, portOffset uint) (*app.App, *app.ManagedClient, uint16, error) {
	if portOffset > 0 {
		return nil, nil, 0, errors.New("netbird listener does not support port ranges")
	}

	a := app.GlobalApp()
	if a == nil {
		return nil, nil, 0, errors.New("netbird app not provisioned")
	}

	port, err := strconv.ParseUint(portRange, 10, 16)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("parse port %q: %w", portRange, err)
	}

	mc, err := a.GetClient(host)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("get netbird client %q: %w", host, err)
	}

	if err := mc.Start(ctx); err != nil {
		if releaseErr := a.ReleaseClient(host); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		return nil, nil, 0, fmt.Errorf("start netbird client %q: %w", host, err)
	}

	return a, mc, uint16(port), nil
}

// listenNetbirdTCP creates a TCP listener on the NetBird virtual interface.
func listenNetbirdTCP(ctx context.Context, _, host, portRange string, portOffset uint, _ net.ListenConfig) (any, error) {
	a, mc, port, err := getClientAndPort(ctx, host, portRange, portOffset)
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf(":%d", port)
	ln, err := mc.Client().ListenTCP(addr)
	if err != nil {
		if releaseErr := a.ReleaseClient(host); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		return nil, fmt.Errorf("listen TCP on netbird %s%s: %w", host, addr, err)
	}

	return &managedListener{
		Listener: ln,
		app:      a,
		node:     host,
	}, nil
}

// listenNetbirdUDP creates a UDP packet listener on the NetBird virtual interface.
func listenNetbirdUDP(ctx context.Context, _, host, portRange string, portOffset uint, _ net.ListenConfig) (any, error) {
	a, mc, port, err := getClientAndPort(ctx, host, portRange, portOffset)
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf(":%d", port)
	pc, err := mc.Client().ListenUDP(addr)
	if err != nil {
		if releaseErr := a.ReleaseClient(host); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		return nil, fmt.Errorf("listen UDP on netbird %s%s: %w", host, addr, err)
	}

	return &managedPacketConn{
		PacketConn: pc,
		app:        a,
		node:       host,
	}, nil
}

// managedListener wraps a net.Listener and releases the client reference
// when closed.
type managedListener struct {
	net.Listener
	app       *app.App
	node      string
	closeOnce sync.Once
	closeErr  error
}

// Close closes the underlying listener and releases the client reference.
func (ml *managedListener) Close() error {
	ml.closeOnce.Do(func() {
		ml.closeErr = ml.Listener.Close()
		if err := ml.app.ReleaseClient(ml.node); err != nil {
			ml.closeErr = errors.Join(ml.closeErr, err)
		}
	})
	return ml.closeErr
}

// managedPacketConn wraps a net.PacketConn and releases the client reference
// when closed.
type managedPacketConn struct {
	net.PacketConn
	app       *app.App
	node      string
	closeOnce sync.Once
	closeErr  error
}

// Close closes the underlying packet connection and releases the client reference.
func (mc *managedPacketConn) Close() error {
	mc.closeOnce.Do(func() {
		mc.closeErr = mc.PacketConn.Close()
		if err := mc.app.ReleaseClient(mc.node); err != nil {
			mc.closeErr = errors.Join(mc.closeErr, err)
		}
	})
	return mc.closeErr
}

// interface guards
var (
	_ io.Closer = (*managedListener)(nil)
	_ io.Closer = (*managedPacketConn)(nil)
)
