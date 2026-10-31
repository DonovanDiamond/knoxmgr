package knox

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/DonovanDiamond/knoxmgr/src/knoxmock"
	"github.com/DonovanDiamond/knoxmgr/src/netproxy"
)

// connectProxy tunnels every CONNECT to target, so a non-bypassed hostname can reach a local mock.
func connectProxy(t *testing.T, target, wantAuth string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantAuth != "" && r.Header.Get("Proxy-Authorization") != wantAuth {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		up, err := net.Dial("tcp", target)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		down, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			up.Close()
			return
		}
		_, _ = down.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		go func() { _, _ = io.Copy(up, down); up.Close() }()
		go func() { _, _ = io.Copy(down, up); down.Close() }()
	}))
	t.Cleanup(s.Close)
	return s
}

func TestProbeThroughProxy(t *testing.T) {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	mock := httptest.NewTLSServer(knoxmock.New("cid", "secret", zerolog.Nop()))
	defer mock.Close()
	pool := x509.NewCertPool()
	pool.AddCert(mock.Certificate())
	_, port, _ := net.SplitHostPort(mock.Listener.Addr().String())
	// example.com is in httptest's certificate and, unlike 127.0.0.1, is not bypassed by the proxy.
	base := "https://example.com:" + port

	const pw = "s3cretpw"
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("bob:"+pw))
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	tests := []struct {
		name    string
		proxy   func() string
		cfg     func(url string) netproxy.Config
		wantMsg string // empty means success
	}{
		{"basic auth success", func() string { return connectProxy(t, mock.Listener.Addr().String(), auth).URL },
			func(u string) netproxy.Config { return netproxy.Config{URL: u, Username: "bob", Password: pw} }, ""},
		{"407", func() string { return connectProxy(t, mock.Listener.Addr().String(), auth).URL },
			func(u string) netproxy.Config {
				return netproxy.Config{URL: u, Username: "bob", Password: "wrong" + pw}
			},
			"Could not reach Knox: proxy authentication required (407)"},
		{"proxy refused", func() string { return deadURL },
			func(u string) netproxy.Config { return netproxy.Config{URL: u, Username: "bob", Password: pw} },
			"Could not reach Knox: proxy refused connection"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := netproxy.Build(tc.cfg(tc.proxy()))
			tr.TLSClientConfig = &tls.Config{RootCAs: pool}
			c := New(config.Knox{BaseURL: base, TokenURL: base + "/emm/oauth/token",
				ClientID: "cid", TenantID: "tid", ClientSecret: "secret"},
				&http.Client{Transport: tr, Timeout: 10 * time.Second})
			err := c.Probe(context.Background())
			if tc.wantMsg == "" {
				if err != nil {
					t.Fatalf("Probe: %v", err)
				}
				return
			}
			var pe *ProbeError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want *ProbeError", err)
			}
			if pe.Stage != "token" || pe.Message != tc.wantMsg {
				t.Errorf("got %q/%q, want token/%q", pe.Stage, pe.Message, tc.wantMsg)
			}
			if strings.Contains(err.Error(), pw) || strings.Contains(err.Error(), "bob") {
				t.Errorf("error leaks proxy credentials: %v", err)
			}
		})
	}
}
