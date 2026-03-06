package srv

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"context"

	"desktriage.davea.me/db/dbgen"
)

const (
	configKeySessionPassword = "session_password"
	configKeyHMACKey         = "hmac_key"

	sessionCookieName = "__session"
	sessionMaxAge     = 30 * 24 * time.Hour // 30 days
)

// shouldUseSecureCookies returns true for non-localhost HTTPS requests.
// Local dev over plain HTTP (localhost/127.0.0.1) is intentionally exempt.
func shouldUseSecureCookies(r *http.Request) bool {
	host := r.Host
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return false
	}

	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}

	return false
}

// LoginData holds template data for the login page.
type LoginData struct {
	Page  string
	Error string
}

// ensureHMACKey loads the HMAC key from the config DB. If not found, it
// generates a random 32-byte key, stores it hex-encoded, and returns it.
func (s *Server) ensureHMACKey(ctx context.Context) ([]byte, error) {
	cfg, err := s.Queries.GetConfig(ctx, configKeyHMACKey)
	if err == nil && cfg.Value != "" {
		key, decErr := hex.DecodeString(cfg.Value)
		if decErr != nil {
			return nil, fmt.Errorf("decode stored hmac key: %w", decErr)
		}
		return key, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("get hmac key config: %w", err)
	}

	// Generate a new 32-byte key.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate hmac key: %w", err)
	}

	hexKey := hex.EncodeToString(key)
	if err := s.Queries.UpsertConfig(ctx, dbgen.UpsertConfigParams{
		Key:         configKeyHMACKey,
		Value:       hexKey,
		Description: "HMAC key for signing session cookies (auto-generated)",
	}); err != nil {
		return nil, fmt.Errorf("store hmac key: %w", err)
	}

	return key, nil
}

// createSessionCookie builds a signed session cookie.
// The value format is "timestamp|hex_hmac".
func createSessionCookie(timestamp int64, hmacKey []byte, secure bool) *http.Cookie {
	tsStr := strconv.FormatInt(timestamp, 10)

	mac := hmac.New(sha256.New, hmacKey)
	mac.Write([]byte(tsStr))
	sig := hex.EncodeToString(mac.Sum(nil))

	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    tsStr + "|" + sig,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	}
}

// validateSessionCookie checks the HMAC signature and ensures the timestamp
// is within maxAge of the current time.
func validateSessionCookie(cookie *http.Cookie, hmacKey []byte, maxAge time.Duration) bool {
	parts := strings.SplitN(cookie.Value, "|", 2)
	if len(parts) != 2 {
		return false
	}

	tsStr, sigHex := parts[0], parts[1]

	// Verify HMAC.
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write([]byte(tsStr))
	expectedSig := mac.Sum(nil)

	actualSig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	if !hmac.Equal(actualSig, expectedSig) {
		return false
	}

	// Check timestamp freshness.
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return false
	}
	issued := time.Unix(ts, 0)
	if time.Since(issued) > maxAge {
		return false
	}

	return true
}

// authMiddleware enforces password-based session authentication.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Check whether a session password has been configured.
		passCfg, err := s.Queries.GetConfig(ctx, configKeySessionPassword)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && passCfg.Value == "") {
			// No password set — skip auth (first-run / open mode).
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			slog.Error("auth: load session password", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Skip auth for public paths.
		path := r.URL.Path
		if path == "/login" || strings.HasPrefix(path, "/static/") || path == "/sw.js" {
			next.ServeHTTP(w, r)
			return
		}

		// Validate the session cookie.
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil {
			hmacKey, keyErr := s.ensureHMACKey(ctx)
			if keyErr == nil && validateSessionCookie(cookie, hmacKey, sessionMaxAge) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Not authenticated.
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		} else {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		}
	})
}

// HandleLogin renders the login page.
func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderTemplateCtx(r.Context(), w, "login.html", LoginData{Page: "login"}); err != nil {
		slog.Warn("render login", "error", err)
	}
}

// HandleLoginSubmit processes the login form submission.
func (s *Server) HandleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	submitted := r.FormValue("password")

	passCfg, err := s.Queries.GetConfig(ctx, configKeySessionPassword)
	if err != nil {
		slog.Error("login: load session password", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if subtle.ConstantTimeCompare([]byte(submitted), []byte(passCfg.Value)) != 1 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		if err := s.renderTemplateCtx(r.Context(), w, "login.html", LoginData{
			Page:  "login",
			Error: "Invalid password.",
		}); err != nil {
			slog.Warn("render login", "error", err)
		}
		return
	}

	// Password correct — issue a session cookie.
	hmacKey, err := s.ensureHMACKey(ctx)
	if err != nil {
		slog.Error("login: ensure hmac key", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, createSessionCookie(time.Now().Unix(), hmacKey, shouldUseSecureCookies(r)))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// HandleLogout clears the session cookie and redirects to the login page.
func (s *Server) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   shouldUseSecureCookies(r),
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
