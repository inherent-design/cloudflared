package ingress

import (
	"bytes"
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

func TestAddPortIfMissing(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"ssh://[::1]", "[::1]:22"},
		{"ssh://[::1]:38", "[::1]:38"},
		{"ssh://abc:38", "abc:38"},
		{"ssh://127.0.0.1:38", "127.0.0.1:38"},
		{"ssh://127.0.0.1", "127.0.0.1:22"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			url1, _ := url.Parse(tc.input)
			addPortIfMissing(url1, 22)
			require.Equal(t, tc.expected, url1.Host)
		})
	}
}

func TestH2cOriginTransport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		h2cOrigin   bool
		http2Origin bool
		scheme      string
		wantErr     bool
		errContains string
	}{
		{"h2c with http origin succeeds", true, false, "http", false, ""},
		{"h2c with https origin errors", true, false, "https", true, "https://"},
		{"h2c with wss origin errors", true, false, "wss", true, "wss://"},
		{"h2c and http2Origin conflict", true, true, "http", true, "cannot both be enabled"},
		{"http2Origin alone is fine", false, true, "https", false, ""},
		{"neither h2c nor http2Origin", false, false, "http", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			log := zerolog.Nop()
			svc := &httpService{url: &url.URL{Scheme: tt.scheme, Host: "localhost:50051"}}
			cfg := OriginRequestConfig{
				H2cOrigin:   tt.h2cOrigin,
				Http2Origin: tt.http2Origin,
			}
			err := svc.start(&log, nil, cfg)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUnixSocketH2cOriginConflict(t *testing.T) {
	t.Parallel()
	log := zerolog.Nop()
	svc := &unixSocketPath{path: "/tmp/cloudflared-h2c-test.sock", scheme: "http"}
	err := svc.start(&log, nil, OriginRequestConfig{
		H2cOrigin:   true,
		Http2Origin: true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot both be enabled")
}

func TestH2cOriginTransportUsesKeepAliveTimeout(t *testing.T) {
	t.Parallel()
	log := zerolog.Nop()
	svc := &httpService{url: &url.URL{Scheme: "http", Host: "localhost:50051"}}
	err := svc.start(&log, nil, OriginRequestConfig{
		H2cOrigin:        true,
		KeepAliveTimeout: config.CustomDuration{Duration: 42 * time.Second},
	})
	require.NoError(t, err)

	transport, ok := svc.transport.(*http2.Transport)
	require.True(t, ok)
	require.Equal(t, 42*time.Second, transport.ReadIdleTimeout)
}

func TestH2cOriginRoundTripUsesHTTP2(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
	})

	type requestInfo struct {
		proto  string
		hasTLS bool
	}
	observedRequest := make(chan requestInfo, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedRequest <- requestInfo{
			proto:  r.Proto,
			hasTLS: r.TLS != nil,
		}
		_, _ = w.Write([]byte("ok"))
	})

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		var server http2.Server
		server.ServeConn(conn, &http2.ServeConnOpts{Handler: handler})
	}()

	log := zerolog.Nop()
	svc := &httpService{url: &url.URL{Scheme: "http", Host: listener.Addr().String()}}
	err = svc.start(&log, nil, OriginRequestConfig{H2cOrigin: true})
	require.NoError(t, err)

	transport, ok := svc.transport.(*http2.Transport)
	require.True(t, ok)
	t.Cleanup(transport.CloseIdleConnections)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/h2c", nil)
	require.NoError(t, err)
	resp, err := svc.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "HTTP/2.0", resp.Proto)

	select {
	case observed := <-observedRequest:
		require.Equal(t, "HTTP/2.0", observed.proto)
		require.False(t, observed.hasTLS)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for h2c origin request")
	}
}

func TestHelloWorldRejectsH2cOrigin(t *testing.T) {
	t.Parallel()
	log := zerolog.Nop()
	svc := &helloWorld{}
	err := svc.start(&log, make(chan struct{}), OriginRequestConfig{
		H2cOrigin: true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "h2cOrigin is enabled")
}

func TestHttp2OriginWithHTTPSchemeWarning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		scheme      string
		http2Origin bool
		wantWarning bool
	}{
		{"http2Origin with http scheme warns", "http", true, true},
		{"http2Origin with https scheme no warning", "https", true, false},
		{"no http2Origin with http scheme no warning", "http", false, false},
		{"no http2Origin with https scheme no warning", "https", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			log := zerolog.New(&buf)
			svc := &httpService{url: &url.URL{Scheme: tt.scheme, Host: "localhost:8080"}}
			cfg := OriginRequestConfig{
				Http2Origin: tt.http2Origin,
				NoTLSVerify: true,
			}
			require.NoError(t, svc.start(&log, nil, cfg))
			if tt.wantWarning {
				require.Contains(t, buf.String(), "http2Origin is enabled")
			} else {
				require.NotContains(t, buf.String(), "http2Origin is enabled")
			}
		})
	}
}
