// Package app provides a Caddy module that manages NetBird client lifecycle,
// configuration, and a shared pool of embed.Client instances.
package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/netbirdio/netbird/client/embed"
	log "github.com/sirupsen/logrus"
	"go.uber.org/zap"

	"github.com/netbirdio/netbird/util"
)

var globalApp atomic.Pointer[App]

// GlobalApp returns the most recently provisioned App instance.
// Used by the listener package to access clients from the RegisterNetwork callback.
func GlobalApp() *App {
	return globalApp.Load()
}

var (
	ErrMissingManagementURL = errors.New("management_url is required (set on node or app level)")
	ErrMissingCredentials   = errors.New("one of setup_key, jwt_token, or private_key is required (set on node or app level)")
)

func init() {
	caddy.RegisterModule(App{})
	httpcaddyfile.RegisterGlobalOption("netbird", parseGlobalOption)
}

// App is the top-level Caddy module that manages NetBird node configurations
// and a shared pool of embed.Client instances.
type App struct {
	// DefaultManagementURL is the default management server URL for all nodes.
	DefaultManagementURL string `json:"management_url,omitempty"`
	// DefaultSetupKey is the default setup key for all nodes.
	DefaultSetupKey string `json:"setup_key,omitempty"`
	// LogLevel sets the NetBird client log level (default: warn).
	LogLevel string `json:"log_level,omitempty"`
	// Nodes is a map of named node configurations.
	Nodes map[string]*Node `json:"nodes,omitempty"`

	pool   *caddy.UsagePool
	logger *zap.Logger
}

// Node is the configuration for a single NetBird client identity.
// The fields mirror the options accepted by the NetBird embedded client
// (embed.Options).
type Node struct {
	// ManagementURL overrides the app-level default.
	ManagementURL string `json:"management_url,omitempty"`
	// SetupKey overrides the app-level default. One of SetupKey,
	// JWTToken, or PrivateKey must be set (directly or via the
	// app-level default setup key).
	SetupKey string `json:"setup_key,omitempty"`
	// JWTToken is used for JWT-based authentication instead of a setup key.
	JWTToken string `json:"jwt_token,omitempty"`
	// PrivateKey is used for direct private key authentication instead of a setup key.
	PrivateKey string `json:"private_key,omitempty"`
	// Hostname is the device name registered in the NetBird network.
	Hostname string `json:"hostname,omitempty"`
	// PreSharedKey is the pre-shared key for the network interface.
	PreSharedKey string `json:"pre_shared_key,omitempty"`
	// LogLevel sets the log level for this client (defaults to info if empty).
	LogLevel string `json:"log_level,omitempty"`
	// ConfigPath is the path to the NetBird config file. If empty, the
	// config is kept in memory and not persisted.
	ConfigPath string `json:"config_path,omitempty"`
	// StatePath is the path to the NetBird state file.
	StatePath string `json:"state_path,omitempty"`
	// WireguardPort is the port for the network interface. Use 0 for a random port.
	WireguardPort *int `json:"wireguard_port,omitempty"`
	// MTU is the MTU for the network interface (valid range 576..8192).
	// If unset, the existing config MTU is preserved, otherwise it defaults to 1280.
	MTU *uint16 `json:"mtu,omitempty"`
	// NoUserspace disables userspace networking mode. Needs admin/root privileges.
	NoUserspace bool `json:"no_userspace,omitempty"`
	// DisableClientRoutes disables the client routes.
	DisableClientRoutes bool `json:"disable_client_routes,omitempty"`
	// DisableIPv6 disables IPv6 overlay addressing.
	DisableIPv6 bool `json:"disable_ipv6,omitempty"`
	// BlockInbound blocks all inbound connections from peers.
	// Defaults to true. Set to false for egress nodes that accept
	// connections from other NetBird peers.
	BlockInbound *bool `json:"block_inbound,omitempty"`
	// BlockLANAccess blocks the peer from reaching the host's LAN
	// (RFC 1918, link-local, loopback) when used as a routing peer.
	BlockLANAccess bool `json:"block_lan_access,omitempty"`
	// DNSLabels defines additional DNS labels configured in the peer.
	DNSLabels []string `json:"dns_labels,omitempty"`
	// PreallocatedBuffersPerPool caps the per-tunnel buffer pool. Zero
	// leaves the pool unbounded. This setting is process-global.
	PreallocatedBuffersPerPool *uint32 `json:"preallocated_buffers_per_pool,omitempty"`
	// MaxBatchSize overrides the number of packets the tunnel reads or
	// writes per syscall. Zero uses the platform default. Process-global.
	MaxBatchSize *uint32 `json:"max_batch_size,omitempty"`
}

// CaddyModule returns the Caddy module information.
func (App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "netbird",
		New: func() caddy.Module { return new(App) },
	}
}

// Provision sets up the app's usage pool, logger, and configures
// the NetBird client's logrus output.
func (a *App) Provision(ctx caddy.Context) error {
	a.logger = ctx.Logger()
	a.pool = caddy.NewUsagePool()

	globalApp.Store(a)

	logLevel := a.LogLevel
	if logLevel == "" {
		logLevel = log.InfoLevel.String()
	}
	if err := util.InitLog(logLevel, util.LogConsole); err != nil {
		return fmt.Errorf("initialize netbird logging: %w", err)
	}

	return nil
}

// Validate ensures each node has a management URL and setup key configured.
func (a *App) Validate() error {
	for name, node := range a.Nodes {
		mgmtURL := node.ManagementURL
		if mgmtURL == "" {
			mgmtURL = a.DefaultManagementURL
		}
		if mgmtURL == "" {
			return fmt.Errorf("node %q: %w", name, ErrMissingManagementURL)
		}

		setupKey := node.SetupKey
		if setupKey == "" {
			setupKey = a.DefaultSetupKey
		}
		if setupKey == "" && node.JWTToken == "" && node.PrivateKey == "" {
			return fmt.Errorf("node %q: %w", name, ErrMissingCredentials)
		}
	}
	return nil
}

// Start is a no-op. Clients are started lazily when transports provision.
func (a *App) Start() error {
	return nil
}

// Stop shuts down all NetBird clients in the pool.
func (a *App) Stop() error {
	globalApp.CompareAndSwap(a, nil)

	var errs []error
	a.pool.Range(func(_ any, val any) bool {
		if err := val.(*ManagedClient).stop(); err != nil {
			errs = append(errs, err)
		}
		return true
	})
	return errors.Join(errs...)
}

// GetClient returns a ref-counted ManagedClient for the named node.
// Each call must be paired with a ReleaseClient call.
func (a *App) GetClient(nodeName string) (*ManagedClient, error) {
	val, loaded, err := a.pool.LoadOrNew(nodeName, func() (caddy.Destructor, error) {
		return a.newManagedClient(nodeName)
	})
	if err != nil {
		return nil, fmt.Errorf("load netbird client %q: %w", nodeName, err)
	}

	mc := val.(*ManagedClient)
	if !loaded {
		a.logger.Info("created netbird client", zap.String("node", nodeName))
	}
	return mc, nil
}

// ReleaseClient decrements the ref count for a node's client.
func (a *App) ReleaseClient(nodeName string) error {
	_, err := a.pool.Delete(nodeName)
	return err
}

// LookupClient returns the ManagedClient for the named node if it exists in the pool.
// Unlike GetClient, it does not create a new client or increment the ref count.
func (a *App) LookupClient(nodeName string) (*ManagedClient, bool) {
	var mc *ManagedClient
	a.pool.Range(func(key, val any) bool {
		if key.(string) == nodeName {
			mc = val.(*ManagedClient)
			return false
		}
		return true
	})
	return mc, mc != nil
}

// newManagedClient resolves the named node's config, builds the NetBird
// embed.Options, and constructs a ManagedClient for it.
func (a *App) newManagedClient(nodeName string) (*ManagedClient, error) {
	node := a.resolveNode(nodeName)

	hostname := node.Hostname
	if hostname == "" {
		hostname = "caddy-" + nodeName
	}

	blockInbound := node.BlockInbound == nil || *node.BlockInbound
	opts := embed.Options{
		DeviceName:          hostname,
		ManagementURL:       node.ManagementURL,
		SetupKey:            node.SetupKey,
		JWTToken:            node.JWTToken,
		PrivateKey:          node.PrivateKey,
		PreSharedKey:        node.PreSharedKey,
		LogLevel:            node.LogLevel,
		ConfigPath:          node.ConfigPath,
		StatePath:           node.StatePath,
		NoUserspace:         node.NoUserspace,
		DisableClientRoutes: node.DisableClientRoutes,
		DisableIPv6:         node.DisableIPv6,
		BlockInbound:        blockInbound,
		BlockLANAccess:      node.BlockLANAccess,
		WireguardPort:       node.WireguardPort,
		MTU:                 node.MTU,
		DNSLabels:           node.DNSLabels,
		Performance: embed.Performance{
			PreallocatedBuffersPerPool: node.PreallocatedBuffersPerPool,
			MaxBatchSize:               node.MaxBatchSize,
		},
	}

	client, err := embed.New(opts)
	if err != nil {
		return nil, fmt.Errorf("create netbird client: %w", err)
	}

	return &ManagedClient{
		client: client,
		logger: a.logger.With(zap.String("node", nodeName)),
	}, nil
}

// resolveNode merges app defaults with the named node config.
func (a *App) resolveNode(name string) Node {
	var node Node
	if n, ok := a.Nodes[name]; ok && n != nil {
		node = *n
	}

	if node.ManagementURL == "" {
		node.ManagementURL = a.DefaultManagementURL
	}
	if node.SetupKey == "" {
		node.SetupKey = a.DefaultSetupKey
	}
	if node.LogLevel == "" {
		node.LogLevel = a.LogLevel
	}
	return node
}

// ManagedClient wraps an embed.Client with lifecycle management and ref-counting.
type ManagedClient struct {
	client  *embed.Client
	logger  *zap.Logger
	started bool
	mu      sync.Mutex
}

// Start starts the NetBird client. Idempotent.
func (mc *ManagedClient) Start(ctx context.Context) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.started {
		return nil
	}

	mc.logger.Info("starting netbird client")
	if err := mc.client.Start(ctx); err != nil {
		return fmt.Errorf("start netbird client: %w", err)
	}
	mc.started = true
	return nil
}

// Client returns the underlying embed.Client.
func (mc *ManagedClient) Client() *embed.Client {
	return mc.client
}

// stop stops the client if running. Idempotent.
func (mc *ManagedClient) stop() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if !mc.started {
		return nil
	}

	mc.logger.Info("stopping netbird client")
	mc.started = false

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := mc.client.Stop(ctx); err != nil {
		return fmt.Errorf("stop netbird client: %w", err)
	}
	return nil
}

// Destruct implements caddy.Destructor for the usage pool.
func (mc *ManagedClient) Destruct() error {
	return mc.stop()
}

// parseGlobalOption parses the top-level netbird config block.
//
//	{
//	    netbird {
//	        management_url https://api.netbird.io:443
//	        setup_key {$NB_SETUP_KEY}
//	        node mynode {
//	            setup_key {$NB_KEY}
//	            hostname my-caddy
//	        }
//	    }
//	}
func parseGlobalOption(d *caddyfile.Dispenser, _ any) (any, error) {
	app := &App{
		Nodes: make(map[string]*Node),
	}

	d.Next() // consume "netbird"

	for d.NextBlock(0) {
		switch d.Val() {
		case "management_url":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			app.DefaultManagementURL = d.Val()

		case "setup_key":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			app.DefaultSetupKey = d.Val()

		case "log_level":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			app.LogLevel = d.Val()

		case "node":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			name := d.Val()
			node, err := parseNode(d)
			if err != nil {
				return nil, err
			}
			app.Nodes[name] = node

		default:
			return nil, d.Errf("unrecognized netbird option: %s", d.Val())
		}
	}

	return httpcaddyfile.App{
		Name:  "netbird",
		Value: caddyconfig.JSON(app, nil),
	}, nil
}

// parseNode parses a single node block within the netbird global option.
func parseNode(d *caddyfile.Dispenser) (*Node, error) {
	node := &Node{}

	for d.NextBlock(1) {
		switch d.Val() {
		case "management_url":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			node.ManagementURL = d.Val()

		case "setup_key":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			node.SetupKey = d.Val()

		case "jwt_token":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			node.JWTToken = d.Val()

		case "private_key":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			node.PrivateKey = d.Val()

		case "hostname":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			node.Hostname = d.Val()

		case "pre_shared_key":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			node.PreSharedKey = d.Val()

		case "log_level":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			node.LogLevel = d.Val()

		case "config_path":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			node.ConfigPath = d.Val()

		case "state_path":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			node.StatePath = d.Val()

		case "wireguard_port":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			port, err := strconv.Atoi(d.Val())
			if err != nil {
				return nil, d.Errf("invalid wireguard_port: %v", err)
			}
			node.WireguardPort = &port

		case "mtu":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			mtu, err := strconv.ParseUint(d.Val(), 10, 16)
			if err != nil {
				return nil, d.Errf("invalid mtu: %v", err)
			}
			v := uint16(mtu)
			node.MTU = &v

		case "no_userspace":
			val, err := parseBoolArg(d)
			if err != nil {
				return nil, err
			}
			node.NoUserspace = val

		case "disable_client_routes":
			val, err := parseBoolArg(d)
			if err != nil {
				return nil, err
			}
			node.DisableClientRoutes = val

		case "disable_ipv6":
			val, err := parseBoolArg(d)
			if err != nil {
				return nil, err
			}
			node.DisableIPv6 = val

		case "block_inbound":
			if !d.NextArg() {
				return nil, d.ArgErr()
			}
			val, err := strconv.ParseBool(d.Val())
			if err != nil {
				return nil, d.Errf("invalid block_inbound: %v", err)
			}
			node.BlockInbound = &val

		case "block_lan_access":
			val, err := parseBoolArg(d)
			if err != nil {
				return nil, err
			}
			node.BlockLANAccess = val

		case "dns_labels":
			labels := d.RemainingArgs()
			if len(labels) == 0 {
				return nil, d.ArgErr()
			}
			node.DNSLabels = labels

		case "preallocated_buffers_per_pool":
			v, err := parseUint32Arg(d)
			if err != nil {
				return nil, err
			}
			node.PreallocatedBuffersPerPool = &v

		case "max_batch_size":
			v, err := parseUint32Arg(d)
			if err != nil {
				return nil, err
			}
			node.MaxBatchSize = &v

		default:
			return nil, d.Errf("unrecognized node option: %s", d.Val())
		}
	}

	return node, nil
}

// parseBoolArg reads and parses a single boolean argument for the current directive.
func parseBoolArg(d *caddyfile.Dispenser) (bool, error) {
	directive := d.Val()
	if !d.NextArg() {
		return false, d.ArgErr()
	}
	val, err := strconv.ParseBool(d.Val())
	if err != nil {
		return false, d.Errf("invalid %s: %v", directive, err)
	}
	return val, nil
}

// parseUint32Arg reads and parses a single uint32 argument for the current directive.
func parseUint32Arg(d *caddyfile.Dispenser) (uint32, error) {
	directive := d.Val()
	if !d.NextArg() {
		return 0, d.ArgErr()
	}
	n, err := strconv.ParseUint(d.Val(), 10, 32)
	if err != nil {
		return 0, d.Errf("invalid %s: %v", directive, err)
	}
	return uint32(n), nil
}

var (
	_ caddy.App         = (*App)(nil)
	_ caddy.Provisioner = (*App)(nil)
	_ caddy.Validator   = (*App)(nil)
)
