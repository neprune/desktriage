package srv

import (
	"crypto/rand"
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

func TestCreateAndValidateSessionCookie(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	cookie := createSessionCookie(now, key, true)

	if !cookie.Secure {
		t.Error("cookie should be Secure when requested")
	}

	if cookie.Name != sessionCookieName {
		t.Errorf("cookie name = %q, want %q", cookie.Name, sessionCookieName)
	}
	if !cookie.HttpOnly {
		t.Error("cookie should be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Error("cookie should have SameSite=Strict")
	}

	// Valid cookie should pass.
	if !validateSessionCookie(cookie, key, 30*24*time.Hour) {
		t.Error("valid cookie should validate")
	}

	// Wrong key should fail.
	wrongKey := make([]byte, 32)
	if _, err := rand.Read(wrongKey); err != nil {
		t.Fatal(err)
	}
	if validateSessionCookie(cookie, wrongKey, 30*24*time.Hour) {
		t.Error("cookie with wrong key should not validate")
	}

	// Expired cookie should fail.
	oldCookie := createSessionCookie(now-86400*31, key, true)
	if validateSessionCookie(oldCookie, key, 30*24*time.Hour) {
		t.Error("expired cookie should not validate")
	}
}

func TestValidateSessionCookie_Tampered(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	cookie := createSessionCookie(time.Now().Unix(), key, true)
	cookie.Value = "9999999999|deadbeef"

	if validateSessionCookie(cookie, key, 30*24*time.Hour) {
		t.Error("tampered cookie should not validate")
	}
}

func TestValidateSessionCookie_MalformedValue(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	for _, val := range []string{"", "nodelimiter", "||double", "notanumber|abc"} {
		c := &http.Cookie{Name: sessionCookieName, Value: val}
		if validateSessionCookie(c, key, 30*24*time.Hour) {
			t.Errorf("malformed value %q should not validate", val)
		}
	}
}

func TestShouldUseSecureCookies(t *testing.T) {
	tests := []struct {
		name string
		host string
		tls  bool
		xfp  string
		want bool
	}{
		{name: "localhost http", host: "localhost:8086", want: false},
		{name: "127.0.0.1 http", host: "127.0.0.1:8086", want: false},
		{name: "localhost https", host: "localhost:8086", tls: true, want: false},
		{name: "remote https direct", host: "desk.example.com", tls: true, want: true},
		{name: "remote https via proxy", host: "desk.example.com", xfp: "https", want: true},
		{name: "remote http", host: "desk.example.com", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &http.Request{Host: tc.host, Header: make(http.Header)}
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if tc.xfp != "" {
				req.Header.Set("X-Forwarded-Proto", tc.xfp)
			}

			got := shouldUseSecureCookies(req)
			if got != tc.want {
				t.Fatalf("shouldUseSecureCookies() = %v, want %v", got, tc.want)
			}
		})
	}
}
