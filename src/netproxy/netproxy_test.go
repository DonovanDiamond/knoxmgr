package netproxy

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		c    Config
		ok   bool
	}{
		{"empty", Config{}, true},
		{"http", Config{URL: "http://proxy.corp:3128"}, true},
		{"https slash", Config{URL: "https://proxy.corp/"}, true},
		{"socks", Config{URL: "socks5://proxy:1080"}, false},
		{"no scheme", Config{URL: "proxy:3128"}, false},
		{"no host", Config{URL: "http://:3128"}, false},
		{"bad port", Config{URL: "http://proxy:abc"}, false},
		{"port range", Config{URL: "http://proxy:70000"}, false},
		{"userinfo", Config{URL: "http://u:p@proxy:3128"}, false},
		{"path", Config{URL: "http://proxy/x"}, false},
		{"query", Config{URL: "http://proxy?a=1"}, false},
		{"fragment", Config{URL: "http://proxy#f"}, false},
		{"bypass ok", Config{Bypass: "localhost, .corp.local,*.x.com,10.0.0.0/8,192.168.1.5,host:8080,[::1]:80,::1,*"}, true},
		{"bypass bad host", Config{Bypass: "bad host"}, false},
		{"bypass bad cidr", Config{Bypass: "10.0.0.0/99"}, false},
		{"bypass bad port", Config{Bypass: "host:99999"}, false},
		{"bypass too long", Config{Bypass: strings.Repeat("a,", 200)}, false},
		{"bypass entry too long", Config{Bypass: strings.Repeat("a", 300)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.c)
			if (err == nil) != tt.ok {
				t.Fatalf("Validate(%+v) = %v, want ok=%v", tt.c, err, tt.ok)
			}
		})
	}
	if err := Validate(Config{URL: "http://u:secretpw@proxy"}); err == nil || strings.Contains(err.Error(), "secretpw") {
		t.Fatalf("error must exist and not echo the password: %v", err)
	}
}

func TestRedact(t *testing.T) {
	tests := []struct{ in, want string }{
		{"http://u:pw@proxy:3128", "http://proxy:3128"},
		{"http://proxy:3128", "http://proxy:3128"},
		{"http://%zz", "(invalid proxy URL)"},
	}
	for _, tt := range tests {
		if got := Redact(tt.in); got != tt.want {
			t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func clearProxyEnv(t *testing.T) {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		t.Setenv(k, "")
	}
}

func TestSourceAndEnvFallback(t *testing.T) {
	clearProxyEnv(t)
	if s, u := Source(Config{}); s != "none" || u != "" {
		t.Fatalf("got %q %q", s, u)
	}
	t.Setenv("HTTP_PROXY", "http://a:b@envhttp:1")
	if s, u := Source(Config{}); s != "env" || u != "http://envhttp:1" {
		t.Fatalf("got %q %q", s, u)
	}
	t.Setenv("HTTPS_PROXY", "http://envhttps:2")
	if s, u := Source(Config{}); s != "env" || u != "http://envhttps:2" {
		t.Fatalf("got %q %q", s, u)
	}
	if s, u := Source(Config{URL: "http://set:3"}); s != "setting" || u != "http://set:3" {
		t.Fatalf("got %q %q", s, u)
	}

	req, _ := http.NewRequest("GET", "https://example.com/", nil)
	got, err := Build(Config{}).Proxy(req)
	if err != nil || got == nil || got.Host != "envhttps:2" {
		t.Fatalf("env proxy = %v, %v", got, err)
	}
	got, err = Build(Config{URL: "http://set:3", Username: "u", Password: "p"}).Proxy(req)
	if err != nil || got == nil || got.Host != "set:3" || got.User.Username() != "u" {
		t.Fatalf("setting proxy = %v, %v", got, err)
	}
	got, _ = Build(Config{URL: "http://set:3", Bypass: "example.com"}).Proxy(req)
	if got != nil {
		t.Fatalf("bypass ignored: %v", got)
	}
}

func TestDescribe(t *testing.T) {
	if _, ok := Describe(io.EOF); ok {
		t.Fatal("non-proxy error reported as proxy failure")
	}
	if _, ok := Describe(nil); ok {
		t.Fatal("nil reported as proxy failure")
	}
}

// proxyServer is a minimal CONNECT proxy that tunnels every request to target,
// whatever host was asked for, so tests can use a hostname that is not bypassed.
func proxyServer(t *testing.T, target, wantAuth string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantAuth != "" && r.Header.Get("Proxy-Authorization") != wantAuth {
			w.Header().Set("Proxy-Authenticate", `Basic realm="t"`)
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
		down.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		go func() { io.Copy(up, down); up.Close() }()
		go func() { io.Copy(down, up); down.Close() }()
	}))
	t.Cleanup(s.Close)
	return s
}

func TestRoundTripThroughProxy(t *testing.T) {
	clearProxyEnv(t)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}))
	defer origin.Close()
	pool := x509.NewCertPool()
	pool.AddCert(origin.Certificate())
	// example.com is in httptest's certificate and, unlike 127.0.0.1, is not auto-bypassed.
	_, port, _ := net.SplitHostPort(origin.Listener.Addr().String())
	target := "https://example.com:" + port + "/"
	const pw = "s3cretpw"
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("bob:"+pw))

	get := func(c Config) (string, error) {
		tr := Build(c)
		tr.TLSClientConfig = &tls.Config{RootCAs: pool}
		resp, err := (&http.Client{Transport: tr}).Get(target)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), nil
	}

	t.Run("basic auth success", func(t *testing.T) {
		p := proxyServer(t, origin.Listener.Addr().String(), auth)
		body, err := get(Config{URL: p.URL, Username: "bob", Password: pw})
		if err != nil || body != "ok" {
			t.Fatalf("body=%q err=%v", body, err)
		}
	})

	t.Run("407", func(t *testing.T) {
		p := proxyServer(t, origin.Listener.Addr().String(), auth)
		_, err := get(Config{URL: p.URL, Username: "bob", Password: "wrong" + pw})
		if err == nil {
			t.Fatal("expected error")
		}
		if msg, ok := Describe(err); !ok || msg != "proxy authentication required (407)" {
			t.Fatalf("Describe = %q, %v (err: %v)", msg, ok, err)
		}
		if strings.Contains(err.Error(), pw) {
			t.Fatalf("password leaked in error: %v", err)
		}
	})

	t.Run("other status", func(t *testing.T) {
		p := proxyServer(t, "127.0.0.1:1", "") // upstream dial fails -> 502
		_, err := get(Config{URL: p.URL})
		if msg, ok := Describe(err); !ok || msg != "proxy rejected the tunnel (Bad Gateway)" {
			t.Fatalf("Describe = %q, %v (err: %v)", msg, ok, err)
		}
	})

	t.Run("refused", func(t *testing.T) {
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		addr := l.Addr().String()
		l.Close()
		_, err := get(Config{URL: "http://" + addr})
		if msg, ok := Describe(err); !ok || msg != "proxy refused connection" {
			t.Fatalf("Describe = %q, %v (err: %v)", msg, ok, err)
		}
	})

	t.Run("swap", func(t *testing.T) {
		p := proxyServer(t, origin.Listener.Addr().String(), "")
		tr := NewTransport(Config{URL: "http://127.0.0.1:1"})
		tr.cur.Load().TLSClientConfig = &tls.Config{RootCAs: pool}
		req, _ := http.NewRequest("GET", target, nil)
		if _, err := tr.RoundTrip(req); err == nil {
			t.Fatal("expected failure via dead proxy")
		}
		tr.Set(Config{URL: p.URL})
		tr.cur.Load().TLSClientConfig = &tls.Config{RootCAs: pool}
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	})
}
