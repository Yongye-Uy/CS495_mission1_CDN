package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"mini-cdn/website/internal/config"
)

const (
	SessionCookieName = "cdn_admin_session"
	CSRFCookieName    = "cdn_csrf_token"
	SessionTTL        = 24 * time.Hour
)

// RateLimiter tracks login attempts per client IP.
type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	maxLimit int
	window   time.Duration
}

func NewRateLimiter(maxLimit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		attempts: make(map[string][]time.Time),
		maxLimit: maxLimit,
		window:   window,
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	var valid []time.Time
	for _, t := range rl.attempts[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.maxLimit {
		rl.attempts[ip] = valid
		return false
	}

	valid = append(valid, now)
	rl.attempts[ip] = valid
	return true
}

func (rl *RateLimiter) Reset(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, ip)
}

// GenerateSessionToken creates a cryptographically signed session token.
func GenerateSessionToken(secret string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s.%s", ts, sig)
}

// ValidateSessionToken verifies the authenticity and expiration of a session token.
func ValidateSessionToken(token string, secret string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}

	tsStr := parts[0]
	sig := parts[1]

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return false
	}

	// Check expiration
	if time.Now().Unix()-ts > int64(SessionTTL.Seconds()) {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(tsStr))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return subtle.ConstantTimeCompare([]byte(sig), []byte(expectedSig)) == 1
}

// GenerateRandomToken generates a hex-encoded cryptographically secure token.
func GenerateRandomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// CSRFMiddleware ensures a CSRF cookie is set and accessible.
func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(CSRFCookieName)
		if err != nil || token == "" {
			token = GenerateRandomToken(32)
			c.SetSameSite(http.SameSiteStrictMode)
			c.SetCookie(CSRFCookieName, token, int(SessionTTL.Seconds()), "/", "", false, false)
		}
		c.Set("csrf_token", token)
		c.Next()
	}
}

// ValidateCSRF verifies the submitted CSRF token against the cookie.
func ValidateCSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		cookieToken, err := c.Cookie(CSRFCookieName)
		if err != nil || cookieToken == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"error":   "CSRF cookie missing or invalid",
			})
			return
		}

		submittedToken := c.GetHeader("X-CSRF-Token")
		if submittedToken == "" {
			submittedToken = c.PostForm("csrf_token")
		}

		if submittedToken == "" || subtle.ConstantTimeCompare([]byte(cookieToken), []byte(submittedToken)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"error":   "CSRF validation failed",
			})
			return
		}

		c.Next()
	}
}

// RequireAdminSession guards administrator pages and actions.
func RequireAdminSession(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(SessionCookieName)
		if err != nil || token == "" || !ValidateSessionToken(token, cfg.SessionSecret) {
			// Check if JSON request
			accept := c.GetHeader("Accept")
			if strings.Contains(accept, "application/json") || c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"error":   "Admin session required. Please sign in.",
				})
				return
			}

			// Redirect browser to login
			c.Redirect(http.StatusFound, "/admin/login?next="+c.Request.URL.RequestURI())
			c.Abort()
			return
		}

		c.Set("is_admin", true)
		c.Next()
	}
}
