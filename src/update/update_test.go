package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestSemver(t *testing.T) {
	tests := []struct {
		current, tag string
		curOK        bool
		want         int
	}{
		{"v0.3.0", "v0.3.0", true, 0},
		{"v0.3.0", "v0.3.1", true, -1},
		{"v0.3.0", "v0.10.0", true, -1},
		{"v1.0.0", "v0.99.99", true, 1},
		{"0.3.0", "v0.3.0", true, 0},
		{"v0.3.0-2-gabc123-dirty", "v0.3.0", true, 0},
		{"v0.3.0-2-gabc123", "v0.3.1", true, -1},
		{"dev", "v0.3.0", false, 0},
		{"abc1234", "v0.3.0", false, 0},
		{"abc1234-dirty", "v0.3.0", false, 0},
		{"v1.2", "v0.3.0", false, 0},
		{"", "v0.3.0", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.current+"_vs_"+tt.tag, func(t *testing.T) {
			cur, ok := parse(currentRe, tt.current)
			if ok != tt.curOK {
				t.Fatalf("parse(%q) ok = %v", tt.current, ok)
			}
			if !ok {
				return
			}
			tag, _ := parse(tagRe, tt.tag)
			if got := compare(cur, tag); got != tt.want {
				t.Fatalf("compare = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTagRejects(t *testing.T) {
	for _, s := range []string{"", "0.3.0", "v0.3", "v0.3.0-rc1", "v0.3.0.1", "vx.y.z", "v1.2.3\n", "v1234567890.0.0", " v1.2.3"} {
		if _, ok := parse(tagRe, s); ok {
			t.Errorf("tag %q accepted", s)
		}
	}
}

func fake(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func checker(version, base string, enabled bool) *Checker {
	return New(version, base, func() bool { return enabled }, nil, zerolog.Nop())
}

func TestCheck(t *testing.T) {
	const releasePath = "/repos/DonovanDiamond/knoxmgr/releases/latest"
	tests := []struct {
		name      string
		version   string
		handler   http.HandlerFunc
		wantLatst string
		wantAvail bool
		wantErr   string
		wantCalls int
	}{
		{"release newer", "v0.3.0", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"tag_name":"v0.4.0"}`))
		}, "v0.4.0", true, "", 1},
		{"release same", "v0.4.0-3-gabc-dirty", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"tag_name":"v0.4.0"}`))
		}, "v0.4.0", false, "", 1},
		{"404 falls back to highest valid tag", "v0.3.0", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == releasePath {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(`[{"name":"v0.9.0-rc1"},{"name":"v0.10.0"},{"name":"v0.9.0"},{"name":"nightly"},{"name":"v99.0"}]`))
		}, "v0.10.0", true, "", 2},
		{"only bad tags", "v0.3.0", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == releasePath {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(`[{"name":"latest"},{"name":"v1.0"}]`))
		}, "", false, "GitHub has no release tags", 2},
		{"bad release tag", "v0.3.0", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"tag_name":"v9.9.9-evil"}`))
		}, "", false, "unexpected release tag", 1},
		{"oversized body", "v0.3.0", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"tag_name":"v0.4.0","pad":"` + strings.Repeat("a", maxBody) + `"}`))
		}, "", false, "too large", 1},
		{"server error hides body", "v0.3.0", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "secret internal detail", http.StatusInternalServerError)
		}, "", false, "HTTP 500", 1},
		{"dev version skipped", "dev", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"tag_name":"v0.4.0"}`))
		}, "", false, "no release version", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			var ua, accept string
			srv := fake(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				ua, accept = r.Header.Get("User-Agent"), r.Header.Get("Accept")
				tt.handler(w, r)
			})
			c := checker(tt.version, srv.URL, true)
			st, err := c.Check(context.Background())
			if calls != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", calls, tt.wantCalls)
			}
			if tt.wantErr == "" {
				if err != nil || st.Error != "" {
					t.Fatalf("err = %v, status error %q", err, st.Error)
				}
			} else if !strings.Contains(st.Error, tt.wantErr) || strings.Contains(st.Error, "secret") {
				t.Fatalf("status error = %q, want %q", st.Error, tt.wantErr)
			}
			if st.Latest != tt.wantLatst || st.UpdateAvailable != tt.wantAvail {
				t.Fatalf("status = %+v", st)
			}
			if tt.wantLatst != "" && st.URL != "https://github.com/DonovanDiamond/knoxmgr/releases/tag/"+tt.wantLatst {
				t.Fatalf("url = %q", st.URL)
			}
			if calls > 0 && (ua != "knoxmgr/"+tt.version || accept != "application/vnd.github+json") {
				t.Fatalf("headers ua=%q accept=%q", ua, accept)
			}
			if got := c.Status(); got != st {
				t.Fatalf("stored %+v != returned %+v", got, st)
			}
		})
	}
}

func TestCheckDisabled(t *testing.T) {
	srv := fake(t, func(w http.ResponseWriter, r *http.Request) { t.Error("disabled checker made a request") })
	if _, err := checker("v0.3.0", srv.URL, false).Check(context.Background()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v", err)
	}
}

func TestWakeChecksEveryTime(t *testing.T) {
	hits := make(chan struct{}, 4)
	srv := fake(t, func(w http.ResponseWriter, r *http.Request) {
		hits <- struct{}{}
		w.Write([]byte(`{"tag_name":"v0.4.0"}`))
	})
	c := checker("v0.3.0", srv.URL, true)
	c.delay = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Start(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	// A wake after a completed check must check again (re-enabling the setting).
	for i := 0; i < 2; i++ {
		c.Wake()
		select {
		case <-hits:
		case <-time.After(5 * time.Second):
			t.Fatalf("wake %d did not trigger a check", i+1)
		}
		for c.Status().CheckedAt.IsZero() {
			runtime.Gosched()
		}
	}
}
