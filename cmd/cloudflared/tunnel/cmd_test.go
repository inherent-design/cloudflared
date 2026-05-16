package tunnel

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/ingress"
)

func TestHostnameFromURI(t *testing.T) {
	assert.Equal(t, "awesome.warptunnels.horse:22", hostnameFromURI("ssh://awesome.warptunnels.horse:22"))
	assert.Equal(t, "awesome.warptunnels.horse:22", hostnameFromURI("ssh://awesome.warptunnels.horse"))
	assert.Equal(t, "awesome.warptunnels.horse:2222", hostnameFromURI("ssh://awesome.warptunnels.horse:2222"))
	assert.Equal(t, "localhost:3389", hostnameFromURI("rdp://localhost"))
	assert.Equal(t, "localhost:3390", hostnameFromURI("rdp://localhost:3390"))
	assert.Empty(t, hostnameFromURI("trash"))
	assert.Empty(t, hostnameFromURI("https://awesomesauce.com"))
}

func TestTunnelH2cOriginFlagRegistered(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	app := &cli.App{
		Writer:   &out,
		Commands: Commands(),
	}

	require.NoError(t, app.Run([]string{"cloudflared", "tunnel", "--" + ingress.H2cOriginFlag, "--help"}))
	require.Contains(t, out.String(), "--"+ingress.H2cOriginFlag)
}
