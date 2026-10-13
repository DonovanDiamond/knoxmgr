// Package knox is a typed client for the Samsung Knox Manage OpenAPI (docs/knox-api/km-api.yml).
package knox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DonovanDiamond/knoxmgr/src/config"
)

const (
	// Refresh this long before the reported expiry so a token never dies in flight.
	tokenSkew = time.Minute
	// Knox reports lifetimes of years; clamp so the Duration arithmetic cannot overflow.
	maxTokenLifetime = 10 * 365 * 24 * time.Hour
	orgCacheTTL      = 60 * time.Second
	maxBody          = 32 << 20
	// apiRoot is appended to cfg.BaseURL, which is just the tenant host.
	apiRoot = "/emm/oapi"
)

// Error is a Knox application-level failure: a resultCode != "0", or an HTTP 4xx answer.
type Error struct {
	Code       string
	Message    string
	HTTPStatus int // 0 for resultCode errors
}

func (e *Error) Error() string {
	if e.HTTPStatus != 0 {
		return fmt.Sprintf("knox: HTTP %d: %s", e.HTTPStatus, e.Message)
	}
	return fmt.Sprintf("knox: resultCode %s: %s", e.Code, e.Message)
}

// ErrUnreachable marks transport-level failures (timeouts, connection errors, token endpoint
// failures) as opposed to Knox answering with an application error.
var ErrUnreachable = errors.New("knox unreachable")

// Client talks to one Knox Manage tenant. It is safe for concurrent use.
type Client struct {
	cfg config.Knox
	hc  *http.Client
	now func() time.Time

	tokMu   sync.Mutex // held across the token fetch so concurrent callers fetch once
	token   string
	expires time.Time

	orgMu   sync.Mutex
	orgs    []Organization
	orgsAt  time.Time
	orgsSet bool
}

// New returns a Client; a nil httpClient uses a client with a 30s timeout.
func New(cfg config.Knox, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{cfg: cfg, hc: httpClient, now: time.Now}
}

// bearer returns a cached token or fetches a new one.
func (c *Client) bearer(ctx context.Context) (string, error) {
	c.tokMu.Lock()
	defer c.tokMu.Unlock()
	if c.token != "" && c.now().Before(c.expires) {
		return c.token, nil
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.cfg.ClientID + "@" + c.cfg.TenantID},
		"client_secret": {c.cfg.ClientSecret.Reveal()},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", errors.New("knox: building token request failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, status, err := c.send(req)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", &tokenStatusError{status}
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil || tr.AccessToken == "" {
		return "", fmt.Errorf("%w: token endpoint returned an unusable response", ErrUnreachable)
	}
	life := min(time.Duration(tr.ExpiresIn)*time.Second, maxTokenLifetime)
	c.token = tr.AccessToken
	c.expires = c.now().Add(life - tokenSkew)
	return c.token, nil
}

// drop forgets tok unless another caller has already replaced it.
func (c *Client) drop(tok string) {
	c.tokMu.Lock()
	defer c.tokMu.Unlock()
	if c.token == tok {
		c.token = ""
	}
}

// send performs the request. Transport errors are reduced to a short fixed description
// because net/http errors embed the full URL.
func (c *Client) send(req *http.Request) ([]byte, int, error) {
	resp, err := c.hc.Do(req)
	if err != nil {
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return nil, 0, ctxErr
		}
		return nil, 0, fmt.Errorf("%w: %s", ErrUnreachable, transportFailure(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: reading response failed", ErrUnreachable)
	}
	return body, resp.StatusCode, nil
}

type envelope[T any] struct {
	ResultCode    string `json:"resultCode"`
	ResultMessage string `json:"resultMessage"`
	ResultValue   T      `json:"resultValue"`
}

// ignored is the T for operations whose resultValue we do not read.
type ignored = json.RawMessage

// call runs one operation. GET operations carry form as the query string, POST
// operations as an urlencoded body. A 401 drops the token and retries once.
func call[T any](ctx context.Context, c *Client, method, path string, form url.Values) (T, error) {
	var zero T
	for attempt := 0; ; attempt++ {
		tok, err := c.bearer(ctx)
		if err != nil {
			return zero, err
		}
		target := strings.TrimRight(c.cfg.BaseURL, "/") + apiRoot + path
		var body io.Reader
		if method == http.MethodGet {
			if len(form) > 0 {
				target += "?" + form.Encode()
			}
		} else {
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequestWithContext(ctx, method, target, body)
		if err != nil {
			return zero, errors.New("knox: building request failed")
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if method != http.MethodGet {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		raw, status, err := c.send(req)
		if err != nil {
			return zero, err
		}
		if status == http.StatusUnauthorized {
			c.drop(tok)
			if attempt == 0 {
				continue
			}
			return zero, fmt.Errorf("%w: %s unauthorized after token refresh", ErrUnreachable, path)
		}
		if status >= 400 && status < 500 {
			return zero, &Error{HTTPStatus: status, Message: path + " rejected the request"}
		}
		if status != http.StatusOK {
			return zero, fmt.Errorf("%w: %s returned HTTP %d", ErrUnreachable, path, status)
		}
		var env envelope[T]
		if err := json.Unmarshal(raw, &env); err != nil {
			return zero, fmt.Errorf("knox: %s returned an unreadable response", path)
		}
		if env.ResultCode != "0" {
			return zero, &Error{Code: env.ResultCode, Message: env.ResultMessage}
		}
		return env.ResultValue, nil
	}
}

// joinIDs builds the ';'-delimited ID lists Knox expects, refusing IDs that
// would smuggle extra entries into the list.
func joinIDs(ids []string) (string, error) {
	if len(ids) == 0 {
		return "", errors.New("knox: at least one id is required")
	}
	for _, id := range ids {
		if id == "" || strings.Contains(id, ";") {
			return "", errors.New("knox: invalid id")
		}
	}
	return strings.Join(ids, ";"), nil
}

// set adds key only when v is non-empty, so unset optional fields stay unset.
func set(form url.Values, key, v string) {
	if v != "" {
		form.Set(key, v)
	}
}
