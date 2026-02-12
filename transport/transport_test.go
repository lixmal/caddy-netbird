package transport

import (
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalCaddyfile_NodeName(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird ingress`)

	var tr Transport
	require.NoError(t, tr.UnmarshalCaddyfile(d))
	assert.Equal(t, "ingress", tr.Node)
	assert.Nil(t, tr.TLS)
}

func TestUnmarshalCaddyfile_DefaultNode(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird`)

	var tr Transport
	require.NoError(t, tr.UnmarshalCaddyfile(d))
	assert.Empty(t, tr.Node, "node should be empty, defaulted later in Provision")
}

func TestUnmarshalCaddyfile_TLS(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird mynode {
		tls
	}`)

	var tr Transport
	require.NoError(t, tr.UnmarshalCaddyfile(d))
	assert.Equal(t, "mynode", tr.Node)
	require.NotNil(t, tr.TLS)
	assert.False(t, tr.TLS.InsecureSkipVerify)
}

func TestUnmarshalCaddyfile_TLSInsecure(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird mynode {
		tls_insecure_skip_verify
	}`)

	var tr Transport
	require.NoError(t, tr.UnmarshalCaddyfile(d))
	require.NotNil(t, tr.TLS)
	assert.True(t, tr.TLS.InsecureSkipVerify)
}

func TestUnmarshalCaddyfile_TLSServerName(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird mynode {
		tls_server_name backend.internal
	}`)

	var tr Transport
	require.NoError(t, tr.UnmarshalCaddyfile(d))
	require.NotNil(t, tr.TLS)
	assert.Equal(t, "backend.internal", tr.TLS.ServerName)
}

func TestUnmarshalCaddyfile_AllTLSOptions(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird ingress {
		tls
		tls_insecure_skip_verify
		tls_server_name vault.internal
	}`)

	var tr Transport
	require.NoError(t, tr.UnmarshalCaddyfile(d))
	assert.Equal(t, "ingress", tr.Node)
	require.NotNil(t, tr.TLS)
	assert.True(t, tr.TLS.InsecureSkipVerify)
	assert.Equal(t, "vault.internal", tr.TLS.ServerName)
}

func TestUnmarshalCaddyfile_UnknownOption(t *testing.T) {
	d := caddyfile.NewTestDispenser(`netbird mynode {
		bogus_option
	}`)

	var tr Transport
	err := tr.UnmarshalCaddyfile(d)
	require.Error(t, err)
}
