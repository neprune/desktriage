# Security audit + code review

Target commit: `7e7a48ebfd3a7358cd0e63cbdf7acd9fab89b13b`  
Date: 2026-03-06

## Scope reviewed

- `config/config.go`
- `srv/auth.go`, `srv/csrf.go`, `srv/ratelimit.go`
- `srv/server.go`, `srv/admin.go`, `srv/state.go`, `srv/ticket.go`
- `srv/templates/layout.html`, `login.html`, `admin_config.html`, `ticket.html`
- tests in `srv/auth_test.go`, `srv/csrf_test.go`, `srv/ratelimit_test.go`

Also ran:

- `go test ./...` ✅
- SAST tooling availability check (`gosec`, `govulncheck`) — not installed in this VM

---

## Executive summary

The commit is a meaningful security improvement (auth, CSRF, throttling) and is generally sound.  
However, there are **several important gaps** before calling it hardened.

### Severity overview

- **High**: 1
- **Medium**: 3
- **Low**: 2

---

## Findings

## 1) Session + CSRF cookies are not marked `Secure` (HIGH)

**Where**

- `srv/auth.go` (`createSessionCookie`, logout clear cookie)
- `srv/csrf.go` (`csrfMiddleware` cookie issuance)

**Details**

Cookies are configured without `Secure: true`.

- Session cookie currently: `HttpOnly`, `SameSite=Strict`, but no `Secure`
- CSRF cookie currently: explicitly `Secure: false`

Without `Secure`, cookies may be sent over plaintext HTTP if a user reaches the app through non-HTTPS paths.

**Recommendation**

- Set `Secure: true` on `__session` and `__csrf`
- Ensure logout/deletion cookies use same security attributes
- If local HTTP dev is needed, gate by env/config (default secure in production)

---

## 2) Session invalidation is incomplete after password change (MEDIUM)

**Where**

- Session design in `srv/auth.go`

**Details**

Session cookies only encode `timestamp|HMAC(timestamp)` with a server-side key.  
Changing `session_password` does **not** invalidate already-issued cookies, so pre-existing sessions remain valid until expiry (30 days).

**Risk**

If a session cookie is stolen, rotating password alone does not evict attacker sessions.

**Recommendation**

Bind session validity to mutable auth state, e.g. include/sign:

- password-version / auth epoch from DB, or
- `hmac_key` rotation on password change, or
- server-side session store with revocation.

---

## 3) No brute-force protection on `/login` (MEDIUM)

**Where**

- `srv/auth.go` (`HandleLoginSubmit`)

**Details**

No attempt throttling/lockout for password guesses. Existing rate limit only protects ticket note creation.

**Recommendation**

Add per-IP and/or global login throttling (e.g., token bucket), plus optional temporary backoff.

---

## 4) Sensitive config value is stored and rendered as plaintext password (MEDIUM)

**Where**

- `config/config.go` (`session_password` in KnownKeys)
- `srv/admin.go` + `srv/templates/admin_config.html` (value rendered back into form)

**Details**

The login password is saved plaintext in DB and echoed into admin HTML (`value="..."`) even with `type=password`.

**Recommendation**

- Store password hash (Argon2id/bcrypt) instead of plaintext
- Do not re-render existing secret values into form fields
- For updates, treat blank as “unchanged” and add explicit “clear/disable auth” control

---

## 5) CSRF token equality check is direct string compare (LOW)

**Where**

- `srv/csrf.go`

**Details**

Token compare uses `submitted != token`. Not typically critical for CSRF tokens, but constant-time compare is a better default for secret comparisons.

**Recommendation**

Use `subtle.ConstantTimeCompare` on byte slices.

---

## 6) In-memory note rate limiter has unbounded key growth (LOW)

**Where**

- `srv/ratelimit.go`

**Details**

`map[int64]time.Time` never evicts keys. Long-running process with high ticket cardinality can grow memory over time.

**Recommendation**

Add periodic cleanup (TTL window) or bounded LRU.

---

## Positive observations

- CSRF middleware is consistently integrated and templates include `_csrf`
- HTMX requests are wired to include `X-CSRF-Token`
- Session cookies are signed with HMAC-SHA256 and validated
- Password comparison uses constant-time compare
- Note endpoint adds practical anti-double-submit throttling
- Tests added for auth/csrf/ratelimit behavior

---

## Suggested priority order

1. **Immediately**: set `Secure` on auth + CSRF cookies
2. Move to hashed password storage and avoid rendering secret values
3. Add login brute-force throttling
4. Add session invalidation semantics on password/key rotation
5. Cleanup: constant-time CSRF compare + limiter eviction
