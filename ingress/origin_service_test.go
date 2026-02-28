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

func TestHttp2OriginWithHTTPSchemeWarning(t *testing.T) {
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
			var buf bytes.Buffer
			log := zerolog.New(&buf)
			svc := &httpService{url: &url.URL{Scheme: tt.scheme, Host: "localhost:8080"}}
			cfg := OriginRequestConfig{Http2Origin: tt.http2Origin}
			// start() will fail at TLS cert loading for https, that's expected for this test
			_ = svc.start(&log, nil, cfg)
			if tt.wantWarning {
				require.Contains(t, buf.String(), "http2Origin is enabled")
			} else {
				require.NotContains(t, buf.String(), "http2Origin is enabled")
			}
		})
	}
}
