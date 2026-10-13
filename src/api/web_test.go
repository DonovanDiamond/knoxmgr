package api

import (
	"io"
	"strings"
	"testing"
)

func TestWebFallback(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		name, path string
		status     int
		body       string // substring the body must contain
	}{
		{"unknown api path", "/api/nope", 404, `"error"`},
		{"unknown nested api path", "/api/knox/nope/deeper", 404, `"error"`},
		{"spa deep link", "/apps/com.facebook.katana", 200, "<html>spa"},
		{"missing asset", "/_app/missing.js", 404, `"error"`},
	} {
		resp := do(t, e.app, "GET", c.path, "", e.token(t, "root"))
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != c.status || !strings.Contains(string(body), c.body) {
			t.Errorf("%s: status %d body %q", c.name, resp.StatusCode, body)
		}
	}
}
