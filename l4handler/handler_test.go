package l4handler

import (
	"net"
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaddyModule(t *testing.T) {
	h := &Handler{}
	info := h.CaddyModule()
	assert.Equal(t, "layer4.handlers.netbird", string(info.ID))
	assert.NotNil(t, info.New)
}

func TestNetworkFromAddr(t *testing.T) {
	tests := []struct {
		name    string
		addr    net.Addr
		want    string
	}{
		{"nil addr", nil, "tcp"},
		{"tcp addr", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80}, "tcp"},
		{"udp addr", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53}, "udp"},
		{"unix addr", &net.UnixAddr{Name: "/tmp/sock", Net: "unix"}, "tcp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, networkFromAddr(tt.addr))
		})
	}
}

func TestUnmarshalCaddyfile_DefaultNode(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird 10.0.0.1:22`)

	var h Handler
	require.NoError(t, h.UnmarshalCaddyfile(d))
	assert.Equal(t, "10.0.0.1:22", h.Upstream)
	assert.Empty(t, h.Node, "node should be empty, defaulted later in Provision")
}

func TestUnmarshalCaddyfile_ExplicitNode(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird 10.0.0.1:5432 dbnode`)

	var h Handler
	require.NoError(t, h.UnmarshalCaddyfile(d))
	assert.Equal(t, "10.0.0.1:5432", h.Upstream)
	assert.Equal(t, "dbnode", h.Node)
}

func TestUnmarshalCaddyfile_MissingUpstream(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird`)

	var h Handler
	err := h.UnmarshalCaddyfile(d)
	require.Error(t, err)
}

func TestUnmarshalCaddyfile_UnexpectedBlock(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird 10.0.0.1:22 {
		bogus
	}`)

	var h Handler
	err := h.UnmarshalCaddyfile(d)
	require.Error(t, err)
}
