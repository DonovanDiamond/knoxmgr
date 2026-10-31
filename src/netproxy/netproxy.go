// Package netproxy builds the single outbound HTTP(S) proxy transport shared by
// the Knox client and the update checker. It imports no internal package.
package netproxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"golang.org/x/net/http/httpproxy"
)

const (
	maxURLLen        = 2048
	maxBypassLen     = 4096
	maxBypassEntry   = 255
	maxBypassEntries = 100
)

// Config is the admin-editable proxy setting. URL never carries userinfo;
// Bypass is a comma-separated NO_PROXY-style list.
type Config struct {
	URL, Username, Password, Bypass string
}

// Validate checks c. An empty URL means "no explicit proxy" and is valid.
func Validate(c Config) error {
	if c.URL != "" {
		if err := validateURL(c.URL); err != nil {
			return err
		}
	}
	return validateBypass(c.Bypass)
}

func validateURL(raw string) error {
	if len(raw) > maxURLLen {
		return errors.New("proxy URL is too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("proxy URL is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("proxy URL scheme must be http or https")
	}
	if u.User != nil {
		return errors.New("proxy URL must not contain credentials; use the username and password fields")
	}
	if u.Hostname() == "" {
		return errors.New("proxy URL must include a host")
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return errors.New("proxy URL port must be a number between 1 and 65535")
		}
	}
	if u.Path != "" && u.Path != "/" {
		return errors.New("proxy URL must not contain a path")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return errors.New("proxy URL must not contain a query or fragment")
	}
	return nil
}

func validateBypass(s string) error {
	if len(s) > maxBypassLen {
		return errors.New("proxy bypass list is too long")
	}
	n := 0
	for _, e := range strings.Split(s, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if n++; n > maxBypassEntries {
			return fmt.Errorf("proxy bypass list has more than %d entries", maxBypassEntries)
		}
		if len(e) > maxBypassEntry {
			return errors.New("a proxy bypass entry is too long")
		}
		if !validBypassEntry(e) {
			return fmt.Errorf("invalid proxy bypass entry %q", e)
		}
	}
	return nil
}

func validBypassEntry(e string) bool {
	if e == "*" {
		return true
	}
	if _, _, err := net.ParseCIDR(e); err == nil {
		return true
	}
	if _, err := netip.ParseAddr(e); err == nil {
		return true
	}
	host := e
	if strings.HasPrefix(e, "[") { // [v6]:port
		h, p, err := net.SplitHostPort(e)
		if err != nil || !validPort(p) {
			return false
		}
		_, err = netip.ParseAddr(h)
		return err == nil
	}
	if i := strings.LastIndexByte(e, ':'); i >= 0 {
		if strings.Count(e, ":") != 1 || !validPort(e[i+1:]) {
			return false
		}
		host = e[:i]
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	host = strings.TrimPrefix(strings.TrimPrefix(host, "*"), ".")
	return validHostname(host)
}

func validPort(p string) bool {
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}

func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, l := range strings.Split(h, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, r := range l {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}

// Redact strips any userinfo from a proxy URL so it is safe to log or show.
func Redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(invalid proxy URL)"
	}
	u.User = nil
	return u.String()
}

// Build returns a clone of http.DefaultTransport routed through the configured
// proxy, or through the environment proxy when no URL is set.
func Build(c Config) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if c.URL == "" {
		t.Proxy = http.ProxyFromEnvironment
		return t
	}
	proxyURL := c.URL
	if u, err := url.Parse(c.URL); err == nil {
		if c.Username != "" || c.Password != "" {
			u.User = url.UserPassword(c.Username, c.Password)
		}
		proxyURL = u.String()
	}
	pf := (&httpproxy.Config{HTTPProxy: proxyURL, HTTPSProxy: proxyURL, NoProxy: c.Bypass}).ProxyFunc()
	t.Proxy = func(r *http.Request) (*url.URL, error) { return pf(r.URL) }
	return t
}

// Transport is a concurrency-safe http.RoundTripper whose underlying proxy
// configuration can be swapped at runtime.
type Transport struct {
	cur atomic.Pointer[http.Transport]
}

func NewTransport(c Config) *Transport {
	t := &Transport{}
	t.cur.Store(Build(c))
	return t
}

// Set swaps in a transport for c and closes the old one's idle connections.
func (t *Transport) Set(c Config) {
	if old := t.cur.Swap(Build(c)); old != nil {
		old.CloseIdleConnections()
	}
}

func (t *Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.cur.Load().RoundTrip(r)
}

// Source reports where the effective proxy comes from: "setting", "env" or
// "none", with the credential-free URL.
func Source(c Config) (source, proxyURL string) {
	if c.URL != "" {
		return "setting", Redact(c.URL)
	}
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if v := os.Getenv(k); v != "" {
			return "env", Redact(v)
		}
	}
	return "none", ""
}

// Describe turns a proxy failure into an admin-readable message. ok is false
// when err is not recognisably caused by the proxy.
func Describe(err error) (msg string, ok bool) {
	if err == nil {
		return "", false
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "proxyconnect" {
		var dns *net.DNSError
		var ne net.Error
		switch {
		case errors.As(err, &dns):
			return "proxy could not be resolved", true
		case errors.As(err, &ne) && ne.Timeout():
			return "proxy timed out", true
		default:
			return "proxy refused connection", true
		}
	}
	// A rejected CONNECT surfaces as a bare status text (net/http drops the code).
	inner := err
	var ue *url.Error
	if errors.As(err, &ue) {
		inner = ue.Err
	}
	text := inner.Error()
	if text == http.StatusText(http.StatusProxyAuthRequired) {
		return "proxy authentication required (407)", true
	}
	for code := 300; code < 600; code++ {
		if text == http.StatusText(code) {
			return "proxy rejected the tunnel (" + text + ")", true
		}
	}
	return "", false
}
