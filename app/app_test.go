package app

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseAndDecode parses a netbird Caddyfile block and decodes the resulting JSON into an App.
func parseAndDecode(t *testing.T, input string) *App {
	t.Helper()

	d := caddyfile.NewTestDispenser(input)
	val, err := parseGlobalOption(d, nil)
	require.NoError(t, err)

	result, ok := val.(httpcaddyfile.App)
	require.True(t, ok, "should return httpcaddyfile.App")
	assert.Equal(t, "netbird", result.Name)

	var app App
	require.NoError(t, json.Unmarshal(result.Value, &app))
	return &app
}

func TestParseGlobalOption(t *testing.T) {
	app := parseAndDecode(t, `netbird {
		management_url https://api.netbird.io:443
		setup_key test-key-123
		log_level debug

		node ingress {
			hostname caddy-ingress
			pre_shared_key psk123
			wireguard_port 51820
		}
	}`)

	assert.Equal(t, "https://api.netbird.io:443", app.DefaultManagementURL)
	assert.Equal(t, "test-key-123", app.DefaultSetupKey)
	assert.Equal(t, "debug", app.LogLevel)

	require.Contains(t, app.Nodes, "ingress")
	node := app.Nodes["ingress"]
	assert.Equal(t, "caddy-ingress", node.Hostname)
	assert.Equal(t, "psk123", node.PreSharedKey)
	require.NotNil(t, node.WireguardPort)
	assert.Equal(t, 51820, *node.WireguardPort)
}

func TestParseGlobalOption_Minimal(t *testing.T) {
	app := parseAndDecode(t, `netbird {
		management_url https://api.netbird.io:443
		setup_key my-key
	}`)

	assert.Equal(t, "https://api.netbird.io:443", app.DefaultManagementURL)
	assert.Equal(t, "my-key", app.DefaultSetupKey)
	assert.Empty(t, app.Nodes)
}

func TestParseGlobalOption_MultipleNodes(t *testing.T) {
	app := parseAndDecode(t, `netbird {
		management_url https://api.netbird.io:443
		setup_key default-key

		node web {
			hostname caddy-web
			setup_key web-key
		}

		node api {
			hostname caddy-api
			management_url https://custom.example.com:443
		}
	}`)

	require.Len(t, app.Nodes, 2)

	web := app.Nodes["web"]
	require.NotNil(t, web)
	assert.Equal(t, "caddy-web", web.Hostname)
	assert.Equal(t, "web-key", web.SetupKey)
	assert.Empty(t, web.ManagementURL, "should not be set, inherits from default")

	api := app.Nodes["api"]
	require.NotNil(t, api)
	assert.Equal(t, "caddy-api", api.Hostname)
	assert.Equal(t, "https://custom.example.com:443", api.ManagementURL)
}

// TestParseGlobalOption_NodeAllOptions verifies that every supported node
// directive is parsed into the corresponding Node field.
func TestParseGlobalOption_NodeAllOptions(t *testing.T) {
	app := parseAndDecode(t, `netbird {
		management_url https://api.netbird.io:443
		setup_key default-key

		node full {
			management_url https://mgmt.example.com:443
			setup_key node-key
			jwt_token my-jwt
			private_key my-priv
			hostname my-caddy
			pre_shared_key secret
			log_level debug
			config_path /etc/netbird/config.json
			state_path /var/lib/netbird/state.json
			wireguard_port 51821
			mtu 1400
			no_userspace true
			disable_client_routes true
			disable_ipv6 true
			block_inbound false
			block_lan_access true
			dns_labels app db cache
			preallocated_buffers_per_pool 512
			max_batch_size 128
		}
	}`)

	node := app.Nodes["full"]
	require.NotNil(t, node)
	assert.Equal(t, "https://mgmt.example.com:443", node.ManagementURL)
	assert.Equal(t, "node-key", node.SetupKey)
	assert.Equal(t, "my-jwt", node.JWTToken)
	assert.Equal(t, "my-priv", node.PrivateKey)
	assert.Equal(t, "my-caddy", node.Hostname)
	assert.Equal(t, "secret", node.PreSharedKey)
	assert.Equal(t, "debug", node.LogLevel)
	assert.Equal(t, "/etc/netbird/config.json", node.ConfigPath)
	assert.Equal(t, "/var/lib/netbird/state.json", node.StatePath)
	require.NotNil(t, node.WireguardPort)
	assert.Equal(t, 51821, *node.WireguardPort)
	require.NotNil(t, node.MTU)
	assert.Equal(t, uint16(1400), *node.MTU)
	assert.True(t, node.NoUserspace)
	assert.True(t, node.DisableClientRoutes)
	assert.True(t, node.DisableIPv6)
	require.NotNil(t, node.BlockInbound)
	assert.False(t, *node.BlockInbound)
	assert.True(t, node.BlockLANAccess)
	assert.Equal(t, []string{"app", "db", "cache"}, node.DNSLabels)
	require.NotNil(t, node.PreallocatedBuffersPerPool)
	assert.Equal(t, uint32(512), *node.PreallocatedBuffersPerPool)
	require.NotNil(t, node.MaxBatchSize)
	assert.Equal(t, uint32(128), *node.MaxBatchSize)
}

func TestParseGlobalOption_UnknownOption(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird {
		unknown_option value
	}`)
	_, err := parseGlobalOption(d, nil)
	require.Error(t, err)
}

func TestParseGlobalOption_UnknownNodeOption(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird {
		node test {
			bogus value
		}
	}`)
	_, err := parseGlobalOption(d, nil)
	require.Error(t, err)
}

func TestParseGlobalOption_InvalidWireguardPort(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird {
		node test {
			wireguard_port not-a-number
		}
	}`)
	_, err := parseGlobalOption(d, nil)
	require.Error(t, err)
}

// TestValidate covers node credential and management URL validation,
// including the setup_key, jwt_token, and private_key alternatives.
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		app     App
		wantErr error
	}{
		{
			name: "valid with defaults",
			app: App{
				DefaultManagementURL: "https://api.netbird.io",
				DefaultSetupKey:      "key",
				Nodes:                map[string]*Node{"web": {}},
			},
		},
		{
			name: "valid with node override",
			app: App{
				Nodes: map[string]*Node{
					"web": {
						ManagementURL: "https://mgmt.example.com",
						SetupKey:      "node-key",
					},
				},
			},
		},
		{
			name: "missing management_url",
			app: App{
				DefaultSetupKey: "key",
				Nodes:           map[string]*Node{"web": {}},
			},
			wantErr: ErrMissingManagementURL,
		},
		{
			name: "missing credentials",
			app: App{
				DefaultManagementURL: "https://api.netbird.io",
				Nodes:                map[string]*Node{"web": {}},
			},
			wantErr: ErrMissingCredentials,
		},
		{
			name: "jwt_token satisfies credentials",
			app: App{
				DefaultManagementURL: "https://api.netbird.io",
				Nodes:                map[string]*Node{"web": {JWTToken: "jwt"}},
			},
		},
		{
			name: "private_key satisfies credentials",
			app: App{
				DefaultManagementURL: "https://api.netbird.io",
				Nodes:                map[string]*Node{"web": {PrivateKey: "priv"}},
			},
		},
		{
			name: "no nodes is valid",
			app: App{
				Nodes: map[string]*Node{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.app.Validate()
			if tt.wantErr != nil {
				require.True(t, errors.Is(err, tt.wantErr), "expected %v, got %v", tt.wantErr, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestResolveNode(t *testing.T) {
	app := &App{
		DefaultManagementURL: "https://default.example.com",
		DefaultSetupKey:      "default-key",
		LogLevel:             "debug",
		Nodes: map[string]*Node{
			"custom": {
				ManagementURL: "https://custom.example.com",
				SetupKey:      "custom-key",
				Hostname:      "my-host",
				LogLevel:      "warn",
			},
			"partial": {
				Hostname: "partial-host",
			},
		},
	}

	t.Run("fully overridden node", func(t *testing.T) {
		node := app.resolveNode("custom")
		assert.Equal(t, "https://custom.example.com", node.ManagementURL)
		assert.Equal(t, "custom-key", node.SetupKey)
		assert.Equal(t, "my-host", node.Hostname)
		assert.Equal(t, "warn", node.LogLevel)
	})

	t.Run("partial node inherits defaults", func(t *testing.T) {
		node := app.resolveNode("partial")
		assert.Equal(t, "https://default.example.com", node.ManagementURL)
		assert.Equal(t, "default-key", node.SetupKey)
		assert.Equal(t, "partial-host", node.Hostname)
		assert.Equal(t, "debug", node.LogLevel)
	})

	t.Run("unknown node gets defaults", func(t *testing.T) {
		node := app.resolveNode("nonexistent")
		assert.Equal(t, "https://default.example.com", node.ManagementURL)
		assert.Equal(t, "default-key", node.SetupKey)
		assert.Equal(t, "", node.Hostname)
	})
}
