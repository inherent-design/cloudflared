package ingress

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"

	"github.com/cloudflare/cloudflared/config"
)

func TestH2cOriginClosesIdleConnectionAndPreservesTrailers(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		require.NoError(t, clientConn.Close())
		require.NoError(t, serverConn.Close())
	})

	serverDone := make(chan struct{})
	requests := make(chan *http.Request, 1)
	go func() {
		defer close(serverDone)
		server := &http2.Server{}
		server.ServeConn(serverConn, &http2.ServeConnOpts{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r
				w.Header().Set("Trailer", "Grpc-Status")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "ok")
				w.Header().Set("Grpc-Status", "0")
			}),
		})
	}()

	log := zerolog.Nop()
	svc := &httpService{url: &url.URL{Scheme: "http", Host: "origin.example:50051"}}
	require.NoError(t, svc.start(&log, nil, OriginRequestConfig{
		H2cOrigin:        true,
		KeepAliveTimeout: config.CustomDuration{Duration: 250 * time.Millisecond},
	}))
	transport, ok := svc.transport.(*http2.Transport)
	require.True(t, ok)
	t.Cleanup(transport.CloseIdleConnections)
	// Use an in-memory origin so the transport contract does not depend on socket permissions.
	transport.DialTLSContext = func(context.Context, string, string, *tls.Config) (net.Conn, error) {
		return clientConn, nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://public.example/rpc", nil)
	require.NoError(t, err)
	resp, err := svc.RoundTrip(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "ok", string(body))
	require.Equal(t, "0", resp.Trailer.Get("Grpc-Status"))
	require.Equal(t, 2, resp.ProtoMajor)
	select {
	case observed := <-requests:
		require.Equal(t, 2, observed.ProtoMajor)
		require.Nil(t, observed.TLS)
	case <-ctx.Done():
		t.Fatal("origin did not receive the h2c request")
	}

	select {
	case <-serverDone:
	case <-ctx.Done():
		t.Fatal("h2c origin connection stayed open beyond keepAliveTimeout")
	}
}
