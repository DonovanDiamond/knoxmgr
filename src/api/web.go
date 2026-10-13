package api

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"mime"
	"path"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// serveWeb serves the embedded UI. Unknown extension-less paths get index.html so the SPA router
// can handle them; a missing file with an extension is a real 404.
func (s *server) serveWeb(c fiber.Ctx) error {
	if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
		return fiber.NewError(fiber.StatusMethodNotAllowed, "method not allowed")
	}
	name := strings.TrimPrefix(path.Clean("/"+c.Path()), "/")
	if name == "" {
		name = "index.html"
	}
	data, err := fs.ReadFile(s.Static, name)
	if err != nil {
		if path.Ext(name) != "" {
			return fiber.NewError(fiber.StatusNotFound, "not found")
		}
		name = "index.html"
		if data, err = fs.ReadFile(s.Static, name); err != nil {
			return fiber.NewError(fiber.StatusNotFound, "not found")
		}
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		c.Set(fiber.HeaderContentType, ct)
	}
	if strings.HasPrefix(name, "_app/immutable/") {
		c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
	} else {
		c.Set(fiber.HeaderCacheControl, "no-cache")
	}
	if path.Ext(name) == ".html" {
		c.Set("Content-Security-Policy", s.csp)
	}
	return c.Send(data)
}

// inlineScript matches <script> elements; those with a src attribute are skipped by the caller.
var inlineScript = regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`)

// contentSecurityPolicy allows scripts from our origin plus the exact inline scripts shipped in the
// built index.html (theme pre-paint and the SvelteKit bootstrap), so no hash can go stale.
func contentSecurityPolicy(static fs.FS) string {
	src := "script-src 'self'"
	if html, err := fs.ReadFile(static, "index.html"); err == nil {
		for _, m := range inlineScript.FindAllSubmatch(html, -1) {
			if strings.Contains(strings.ToLower(string(m[1])), "src=") {
				continue
			}
			sum := sha256.Sum256(m[2])
			src += " 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		}
	}
	return "default-src 'self'; " + src + "; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'"
}
