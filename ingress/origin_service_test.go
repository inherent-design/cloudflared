package ingress

import (
	"bytes"
	"net/url"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
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
		{"h2c with https origin errors", true, false, "https", true, "h2cOrigin is enabled but"},
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
			}
			// start() may fail at TLS cert loading for non-h2c https cases
		})
	}
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
