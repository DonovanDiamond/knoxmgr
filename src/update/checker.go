package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	DefaultAPI     = "https://api.github.com"
	repo           = "DonovanDiamond/knoxmgr"
	releasePage    = "https://github.com/" + repo + "/releases/tag/"
	startDelay     = time.Minute
	interval       = 24 * time.Hour
	requestTimeout = 10 * time.Second
	maxBody        = 1 << 20
)

// ErrDisabled means the admin has turned update checks off.
var ErrDisabled = errors.New("update checks are disabled")

// Status is the outcome of the latest check, kept in memory only.
type Status struct {
	Latest          string
	URL             string
	UpdateAvailable bool
	CheckedAt       time.Time
	Error           string // short and fixed; never a response body
}

// Checker polls GitHub while enabled() is true and the running version is a release.
type Checker struct {
	version string
	current semver
	isRel   bool
	base    string
	enabled func() bool
	client  *http.Client
	log     zerolog.Logger
	delay   time.Duration
	wake    chan struct{}

	mu     sync.Mutex
	status Status
}

// New returns a Checker. An empty base means the public GitHub API.
func New(version, base string, enabled func() bool, log zerolog.Logger) *Checker {
	if base == "" {
		base = DefaultAPI
	}
	cur, ok := parse(currentRe, version)
	return &Checker{version: version, current: cur, isRel: ok, base: strings.TrimRight(base, "/"), enabled: enabled,
		client: &http.Client{Timeout: requestTimeout}, log: log, delay: startDelay, wake: make(chan struct{}, 1)}
}

// Status returns the last result.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Wake asks the loop to check now; Check does nothing while updates are disabled. It never blocks.
func (c *Checker) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Start runs until ctx ends: one check after a short delay, then one every 24 hours.
func (c *Checker) Start(ctx context.Context) {
	timer := time.NewTimer(c.delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			c.run(ctx)
			timer.Reset(interval)
		case <-c.wake:
			c.run(ctx)
		}
	}
}

func (c *Checker) run(ctx context.Context) {
	if _, err := c.Check(ctx); err != nil && !errors.Is(err, ErrDisabled) {
		c.log.Warn().Err(err).Msg("update check failed")
	}
}

// Check queries GitHub now. It returns ErrDisabled when the setting is off. A development build
// or unparseable version is not queried and is reported in Status.Error.
func (c *Checker) Check(ctx context.Context) (Status, error) {
	if !c.enabled() {
		return c.Status(), ErrDisabled
	}
	if !c.isRel {
		return c.store(Status{Error: "this build has no release version"}), nil
	}
	tag, err := c.latestTag(ctx)
	st := Status{CheckedAt: time.Now().UTC()}
	if err != nil {
		st.Error = err.Error()
		return c.store(st), err
	}
	latest, _ := parse(tagRe, tag)
	st.Latest, st.URL, st.UpdateAvailable = tag, releasePage+tag, compare(latest, c.current) > 0
	return c.store(st), nil
}

func (c *Checker) store(s Status) Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = s
	return s
}

// latestTag prefers the latest release and falls back to the highest tag when there is none.
func (c *Checker) latestTag(ctx context.Context) (string, error) {
	var rel struct {
		TagName string `json:"tag_name"`
	}
	status, err := c.get(ctx, "/repos/"+repo+"/releases/latest", &rel)
	switch {
	case err != nil:
		return "", err
	case status == http.StatusOK:
		if _, ok := parse(tagRe, rel.TagName); !ok {
			return "", errors.New("GitHub returned an unexpected release tag")
		}
		return rel.TagName, nil
	case status != http.StatusNotFound:
		return "", fmt.Errorf("GitHub returned HTTP %d", status)
	}

	var tags []struct {
		Name string `json:"name"`
	}
	if status, err = c.get(ctx, "/repos/"+repo+"/tags?per_page=100", &tags); err != nil {
		return "", err
	} else if status != http.StatusOK {
		return "", fmt.Errorf("GitHub returned HTTP %d", status)
	}
	best, found := semver{}, ""
	for _, t := range tags {
		if v, ok := parse(tagRe, t.Name); ok && (found == "" || compare(v, best) > 0) {
			best, found = v, t.Name
		}
	}
	if found == "" {
		return "", errors.New("GitHub has no release tags")
	}
	return found, nil
}

// get decodes a 200 body into out; other statuses are returned for the caller to judge.
func (c *Checker) get(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, errors.New("invalid update URL")
	}
	req.Header.Set("User-Agent", "knoxmgr/"+c.version)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, errors.New("could not reach GitHub")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return 0, errors.New("could not read the GitHub response")
	}
	if len(body) > maxBody {
		return 0, errors.New("GitHub response too large")
	}
	if err := json.Unmarshal(body, out); err != nil {
		return 0, errors.New("GitHub returned an unreadable response")
	}
	return resp.StatusCode, nil
}
