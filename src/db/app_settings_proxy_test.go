package db

import (
	"context"
	"strings"
	"testing"
)

func TestProxySettings(t *testing.T) {
	ctx := context.Background()
	const sealed = "SEALED-PROXY-ciphertext"
	base := ProxySettings{URL: "http://proxy:3128", Username: "svc", PasswordCipher: sealed, Bypass: "localhost,10.0.0.0/8"}

	r := NewAppSettings(testDB(t))
	if got, err := r.GetProxy(ctx); err != nil || got != (ProxySettings{}) {
		t.Fatalf("default = %+v, %v", got, err)
	}

	tests := []struct {
		name          string
		next          ProxySettings
		secretChanged bool
		wantPassword  string
		wantPrev      string
	}{
		{"first save with password", base, true, `"password":"changed"`, `"url":""`},
		{"password kept", ProxySettings{URL: "http://other:80", Username: "svc", PasswordCipher: sealed, Bypass: "x"}, false, `"password":"unchanged"`, `"url":"http://proxy:3128"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := r.SaveProxy(ctx, tc.next, tc.secretChanged, nil, "root"); err != nil {
				t.Fatal(err)
			}
			if got, err := r.GetProxy(ctx); err != nil || got != tc.next {
				t.Fatalf("got %+v, %v; want %+v", got, err, tc.next)
			}
			entries, err := NewAudit(r.db).ListRecent(ctx, 1)
			if err != nil || len(entries) != 1 {
				t.Fatalf("audit = %v, %v", entries, err)
			}
			e := entries[0]
			if e.Action != "settings.proxy" || e.PreviousValue == nil || e.NewValue == nil {
				t.Fatalf("audit entry = %+v", e)
			}
			for _, v := range []string{*e.PreviousValue, *e.NewValue} {
				if strings.Contains(v, "SEALED") || strings.Contains(v, "ciphertext") {
					t.Fatalf("audit leaks the cipher: %s", v)
				}
			}
			if !strings.Contains(*e.NewValue, tc.wantPassword) || !strings.Contains(*e.PreviousValue, tc.wantPrev) || strings.Contains(*e.PreviousValue, "password") {
				t.Fatalf("audit prev %s new %s", *e.PreviousValue, *e.NewValue)
			}
		})
	}
}
