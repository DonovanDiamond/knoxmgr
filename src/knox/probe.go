package knox

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/DonovanDiamond/knoxmgr/src/netproxy"
)

// tokenStatusError is the token endpoint answering with a non-200 status. It is an ErrUnreachable
// so ordinary callers treat it like any other token failure.
type tokenStatusError struct{ status int }

func (e *tokenStatusError) Error() string {
	return fmt.Sprintf("knox unreachable: token endpoint returned HTTP %d", e.status)
}
func (e *tokenStatusError) Unwrap() error { return ErrUnreachable }

// transportFailure names why a request never got an answer, without the URL net/http embeds.
func transportFailure(err error) string {
	var dns *net.DNSError
	var unknownCA x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var host x509.HostnameError
	var ne net.Error
	if msg, ok := netproxy.Describe(err); ok {
		return msg
	}
	switch {
	case errors.As(err, &dns):
		return "DNS lookup failed"
	case errors.As(err, &unknownCA), errors.As(err, &invalid), errors.As(err, &host), strings.Contains(err.Error(), "tls:"):
		return "TLS handshake failed"
	case errors.As(err, &ne) && ne.Timeout():
		return "timed out"
	case errors.As(err, &ne):
		return "connection failed"
	}
	return "request failed"
}

// ProbeError says which step of Probe failed and why, in words fit to show an admin.
type ProbeError struct {
	Stage   string // "token" or "api"
	Message string
}

func (e *ProbeError) Error() string { return e.Stage + ": " + e.Message }

// Probe fetches a token and then lists organizations once, returning a *ProbeError on failure.
// Use a fresh Client: a cached token would skip the credential check.
func (c *Client) Probe(ctx context.Context) error {
	if _, err := c.bearer(ctx); err != nil {
		return &ProbeError{"token", probeMessage(ctx, err, "the token endpoint")}
	}
	if _, err := c.SelectOrgList(ctx, ""); err != nil {
		return &ProbeError{"api", probeMessage(ctx, err, "the API")}
	}
	return nil
}

func probeMessage(ctx context.Context, err error, what string) string {
	var ts *tokenStatusError
	var ke *Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), ctx.Err() != nil:
		return "Timed out waiting for Knox"
	case errors.As(err, &ts) && (ts.status == http.StatusBadRequest || ts.status == http.StatusUnauthorized || ts.status == http.StatusForbidden):
		return "Knox rejected the credentials: check the client ID, client secret and tenant ID"
	case errors.As(err, &ke) && ke.HTTPStatus == 0:
		return fmt.Sprintf("Knox answered with error %s: %s", ke.Code, ke.Message)
	case errors.As(err, &ke):
		return fmt.Sprintf("Knox answered HTTP %d from %s", ke.HTTPStatus, what)
	case errors.Is(err, ErrUnreachable):
		// ErrUnreachable text is fixed wording with no URL or secret, written for this.
		return "Could not reach Knox: " + strings.TrimPrefix(err.Error(), ErrUnreachable.Error()+": ")
	}
	return "Unexpected response from Knox"
}
