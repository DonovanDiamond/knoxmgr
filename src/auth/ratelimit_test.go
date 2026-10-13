package auth

import (
	"errors"
	"testing"
	"time"
)

func TestLoginLimiter(t *testing.T) {
	clock := time.Now()
	l := NewLoginLimiter()
	l.now = func() time.Time { return clock }

	fail := func(ip, user string, n int) {
		for range n {
			l.Failure(ip, user)
		}
	}
	fail("1.1.1.1", "Bob", 4)
	if err := l.Allow("1.1.1.1", "bob"); err != nil {
		t.Fatalf("4 failures should still be allowed: %v", err)
	}
	fail("1.1.1.1", "BOB", 1)
	if err := l.Allow("1.1.1.1", "bob"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
	if err := l.Allow("2.2.2.2", "bob"); err != nil {
		t.Fatalf("other IP must be independent: %v", err)
	}
	if err := l.Allow("1.1.1.1", "alice"); err != nil {
		t.Fatalf("other user must be independent: %v", err)
	}

	clock = clock.Add(defaultWindow)
	if err := l.Allow("1.1.1.1", "bob"); err != nil {
		t.Fatalf("window elapsed, want allowed: %v", err)
	}

	fail("1.1.1.1", "bob", 4)
	l.Success("1.1.1.1", "bob")
	fail("1.1.1.1", "bob", 4)
	if err := l.Allow("1.1.1.1", "bob"); err != nil {
		t.Fatalf("success must reset the count: %v", err)
	}
}

func TestLoginLimiterBounded(t *testing.T) {
	clock := time.Now()
	l := NewLoginLimiter()
	l.now = func() time.Time { return clock }

	// One shared IP key plus maxLimiterKeys-1 IP+username keys.
	for i := range maxLimiterKeys - 1 {
		l.Failure("ip", string(rune('a'+i%26))+time.Duration(i).String())
	}
	if len(l.entries) != maxLimiterKeys {
		t.Fatalf("entries = %d", len(l.entries))
	}
	clock = clock.Add(defaultWindow)
	l.Failure("ip", "fresh")
	if len(l.entries) != 2 {
		t.Fatalf("expired entries should be pruned, have %d", len(l.entries))
	}
	for i := range maxLimiterKeys {
		l.Failure("ip", "u"+time.Duration(i).String())
	}
	l.Failure("ip", "overflow")
	if len(l.entries) > maxLimiterKeys {
		t.Fatalf("map grew past the bound: %d", len(l.entries))
	}
}

func TestNormaliseUsername(t *testing.T) {
	tests := []struct{ in, want string }{
		{"bob", "bob"},
		{"  BoB ", "bob"},
		{`CORP\Bob`, "bob"},
		{"Bob@corp.example", "bob"},
		{`CORP\Bob@corp.example`, "bob"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normaliseUsername(tt.in); got != tt.want {
			t.Errorf("normaliseUsername(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoginLimiterVariantsShareCounter(t *testing.T) {
	l := NewLoginLimiter()
	for _, u := range []string{"bob", " BOB", `CORP\bob`, "bob@corp.example", "Bob"} {
		l.Failure("1.1.1.1", u)
	}
	if err := l.Allow("1.1.1.1", "bob"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
}

func TestLoginLimiterPerIP(t *testing.T) {
	tests := []struct {
		name     string
		failures int // each against a distinct username from one IP
		wantErr  error
	}{
		{"just under the IP limit", defaultIPMaxFailures - 1, nil},
		{"at the IP limit", defaultIPMaxFailures, ErrRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewLoginLimiter()
			for i := range tt.failures {
				l.Failure("1.1.1.1", "user"+time.Duration(i).String())
			}
			if err := l.Allow("1.1.1.1", "fresh"); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err := l.Allow("2.2.2.2", "fresh"); err != nil {
				t.Fatalf("other IP must be independent: %v", err)
			}
		})
	}

	t.Run("success keeps the IP counter", func(t *testing.T) {
		l := NewLoginLimiter()
		for i := range defaultIPMaxFailures {
			l.Failure("1.1.1.1", "user"+time.Duration(i).String())
		}
		l.Success("1.1.1.1", "user0s")
		if err := l.Allow("1.1.1.1", "fresh"); !errors.Is(err, ErrRateLimited) {
			t.Fatalf("want ErrRateLimited, got %v", err)
		}
	})
}

func TestLoginLimiterEvictsOldest(t *testing.T) {
	clock := time.Now()
	l := NewLoginLimiter()
	l.now = func() time.Time { return clock }

	// Distinct window starts, all inside the window. Each Failure adds an IP+user key and the
	// shared IP key, so use a distinct IP per call to fill the map with 2 keys each.
	const calls = maxLimiterKeys / 2
	for i := range calls {
		clock = clock.Add(time.Millisecond)
		l.Failure("ip"+time.Duration(i).String(), "u")
	}
	if len(l.entries) != maxLimiterKeys {
		t.Fatalf("entries = %d, want %d", len(l.entries), maxLimiterKeys)
	}
	oldest := limiterKey("ip"+time.Duration(0).String(), "u")
	newest := limiterKey("ip"+time.Duration(calls-1).String(), "u")

	clock = clock.Add(time.Millisecond)
	l.Failure("overflow", "u")

	if len(l.entries) > maxLimiterKeys-evictBatch+2 || len(l.entries) < maxLimiterKeys-evictBatch {
		t.Fatalf("entries = %d, want about %d after evicting the oldest batch", len(l.entries), maxLimiterKeys-evictBatch)
	}
	if _, ok := l.entries[oldest]; ok {
		t.Error("oldest entry should have been evicted")
	}
	if _, ok := l.entries[newest]; !ok {
		t.Error("newest entry must survive eviction")
	}
	if _, ok := l.entries[limiterKey("overflow", "u")]; !ok {
		t.Error("the new entry must be stored")
	}
}
