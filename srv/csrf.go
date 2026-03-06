package srv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

const csrfCookieName = "__csrf"

// contextKey is an unexported type for CSRF context keys, preventing collisions.
type csrfContextKey struct{}

// generateCSRFToken returns a 32-byte random hex-encoded token.
func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// csrfTokenFromContext retrieves the CSRF token stored in the request context.
func csrfTokenFromContext(ctx context.Context) string {
	if tok, ok := ctx.Value(csrfContextKey{}).(string); ok {
		return tok
	}
	return ""
}

// csrfMiddleware implements the double-submit cookie CSRF pattern.
func csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read existing token from cookie, or generate a new one.
		var token string
		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" {
			// No cookie present — generate a fresh token.
			token, err = generateCSRFToken()
			if err != nil {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     csrfCookieName,
				Value:    token,
				Path:     "/",
				HttpOnly: false, // JS needs to read it
				Secure:   shouldUseSecureCookies(r),
				SameSite: http.SameSiteStrictMode,
			})
		} else {
			token = cookie.Value
		}

		// Store token in context for templates / handlers.
		ctx := context.WithValue(r.Context(), csrfContextKey{}, token)
		r = r.WithContext(ctx)

		// Safe methods: no validation required.
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		// Unsafe methods: validate the double-submit.
		// Try the form field first, then the header.
		_ = r.ParseForm()
		submitted := r.FormValue("_csrf")
		if submitted == "" {
			submitted = r.Header.Get("X-CSRF-Token")
		}

		if submitted == "" || submitted != token {
			http.Error(w, "CSRF validation failed", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
