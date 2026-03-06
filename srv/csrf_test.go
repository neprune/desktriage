package srv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFMiddleware_GET_SetsCookie(t *testing.T) {
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET returned %d, want 200", rr.Code)
	}

	// Should have set the CSRF cookie.
	found := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == csrfCookieName {
			found = true
			if len(c.Value) != 64 { // 32 bytes hex = 64 chars
				t.Errorf("cookie value length = %d, want 64", len(c.Value))
			}
			if c.Secure {
				t.Error("csrf cookie should not be Secure for localhost dev")
			}
		}
	}
	if !found {
		t.Error("CSRF cookie not set on GET")
	}
}

func TestCSRFMiddleware_POST_WithoutToken_Returns403(t *testing.T) {
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/something", strings.NewReader("body=test"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "abc123"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("POST without CSRF token returned %d, want 403", rr.Code)
	}
}

func TestCSRFMiddleware_POST_WithFormField_Succeeds(t *testing.T) {
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	token := "valid-token-for-test"
	req := httptest.NewRequest(http.MethodPost, "/something", strings.NewReader("_csrf="+token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST with valid CSRF form field returned %d, want 200", rr.Code)
	}
}

func TestCSRFMiddleware_PUT_WithHeader_Succeeds(t *testing.T) {
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	token := "valid-token-for-test"
	req := httptest.NewRequest(http.MethodPut, "/something", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	req.Header.Set("X-CSRF-Token", token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("PUT with valid X-CSRF-Token header returned %d, want 200", rr.Code)
	}
}

func TestCSRFMiddleware_POST_MismatchedToken_Returns403(t *testing.T) {
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/something", strings.NewReader("_csrf=wrong-token"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "correct-token"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("POST with mismatched CSRF token returned %d, want 403", rr.Code)
	}
}

func TestCSRFTokenFromContext(t *testing.T) {
	var gotToken string
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = csrfTokenFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if gotToken == "" {
		t.Error("csrfTokenFromContext returned empty string")
	}
}

func TestCSRFMiddleware_GET_SetsSecureCookieForHTTPSProxy(t *testing.T) {
	handler := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "desk.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET returned %d, want 200", rr.Code)
	}

	for _, c := range rr.Result().Cookies() {
		if c.Name == csrfCookieName {
			if !c.Secure {
				t.Fatal("csrf cookie should be Secure for HTTPS/proxied HTTPS")
			}
			return
		}
	}
	t.Fatal("CSRF cookie not set")
}
