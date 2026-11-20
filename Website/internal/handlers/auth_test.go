package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"mini-cdn/website/internal/config"
	"mini-cdn/website/internal/middleware"
)

func setupAuthTestEngine(t *testing.T) (*gin.Engine, *WebHandler, string) {
	gin.SetMode(gin.TestMode)
	tmplDir := createTestTemplates(t)

	// Add login.html to template dir
	loginContent := `<!DOCTYPE html><html><body><h1>Login Page</h1>{{ if .Error }}<p>{{ .Error }}</p>{{ end }}</body></html>`
	_ = os.WriteFile(filepath.Join(tmplDir, "login.html"), []byte(loginContent), 0644)

	pwdHash, _ := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	cfg := &config.Config{
		Port:              "3000",
		ApiBaseURL:        "http://localhost:8080",
		AdminPasswordHash: string(pwdHash),
		AdminAPIKey:       "test-secret-key-xyz",
		SessionSecret:     "unit-test-session-secret-999",
	}

	handler, err := NewWebHandler(cfg)
	if err != nil {
		t.Fatalf("NewWebHandler failed: %v", err)
	}

	r := gin.New()
	r.Use(middleware.CSRFMiddleware())
	r.LoadHTMLGlob(filepath.Join(tmplDir, "*.html"))

	r.GET("/admin/login", handler.LoginPage)
	r.POST("/admin/login", middleware.ValidateCSRF(), handler.HandleLogin)
	r.POST("/admin/logout", handler.HandleLogout)

	admin := r.Group("")
	admin.Use(middleware.RequireAdminSession(cfg))
	{
		admin.GET("/nodes/add", handler.AddNodePage)
		admin.POST("/nodes/add", middleware.ValidateCSRF(), handler.HandleAddNode)
	}

	return r, handler, tmplDir
}

func TestAuth_SessionAndProtection(t *testing.T) {
	r, _, tmplDir := setupAuthTestEngine(t)
	defer os.RemoveAll(tmplDir)

	// 1. Unauthenticated GET /nodes/add redirects to /admin/login
	req := httptest.NewRequest(http.MethodGet, "/nodes/add", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("Expected 302 redirect for unauthenticated browser, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "/admin/login") {
		t.Fatalf("Expected redirect to /admin/login, got %s", rec.Header().Get("Location"))
	}

	// 2. Unauthenticated JSON request returns 401 Unauthorized
	reqJSON := httptest.NewRequest(http.MethodGet, "/nodes/add", nil)
	reqJSON.Header.Set("Accept", "application/json")
	recJSON := httptest.NewRecorder()
	r.ServeHTTP(recJSON, reqJSON)

	if recJSON.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for JSON request, got %d", recJSON.Code)
	}

	// 3. Obtain CSRF token from initial page visit
	csrfReq := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	csrfRec := httptest.NewRecorder()
	r.ServeHTTP(csrfRec, csrfReq)

	csrfCookie := csrfRec.Result().Cookies()
	var csrfVal string
	for _, c := range csrfCookie {
		if c.Name == middleware.CSRFCookieName {
			csrfVal = c.Value
		}
	}
	if csrfVal == "" {
		t.Fatalf("Expected CSRF cookie to be set")
	}

	// 4. Invalid Login credentials returns 401
	formWrong := url.Values{}
	formWrong.Set("password", "wrongpassword")
	formWrong.Set("csrf_token", csrfVal)
	reqWrong := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(formWrong.Encode()))
	reqWrong.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqWrong.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: csrfVal})
	recWrong := httptest.NewRecorder()
	r.ServeHTTP(recWrong, reqWrong)

	if recWrong.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized on wrong password, got %d", recWrong.Code)
	}

	// 5. Valid Login credentials returns 302 and sets session cookie
	formValid := url.Values{}
	formValid.Set("password", "secret123")
	formValid.Set("csrf_token", csrfVal)
	reqValid := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(formValid.Encode()))
	reqValid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqValid.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: csrfVal})
	recValid := httptest.NewRecorder()
	r.ServeHTTP(recValid, reqValid)

	if recValid.Code != http.StatusFound {
		t.Fatalf("Expected 302 redirect after login, got %d", recValid.Code)
	}

	var sessionVal string
	for _, c := range recValid.Result().Cookies() {
		if c.Name == middleware.SessionCookieName {
			sessionVal = c.Value
		}
	}
	if sessionVal == "" {
		t.Fatalf("Expected session cookie to be set after successful login")
	}

	// 6. Access /nodes/add with valid session cookie succeeds (200 OK)
	reqAuth := httptest.NewRequest(http.MethodGet, "/nodes/add", nil)
	reqAuth.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: sessionVal})
	recAuth := httptest.NewRecorder()
	r.ServeHTTP(recAuth, reqAuth)

	if recAuth.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK with valid session, got %d", recAuth.Code)
	}

	// 7. Logout clears session cookie
	logoutReq := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	logoutRec := httptest.NewRecorder()
	r.ServeHTTP(logoutRec, logoutReq)

	var cleared bool
	for _, c := range logoutRec.Result().Cookies() {
		if c.Name == middleware.SessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("Expected session cookie to be deleted on logout")
	}
}

func TestAuth_RateLimiting(t *testing.T) {
	r, _, tmplDir := setupAuthTestEngine(t)
	defer os.RemoveAll(tmplDir)

	// Obtain CSRF token
	csrfReq := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	csrfRec := httptest.NewRecorder()
	r.ServeHTTP(csrfRec, csrfReq)

	var csrfVal string
	for _, c := range csrfRec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			csrfVal = c.Value
		}
	}

	form := url.Values{}
	form.Set("password", "badpass")
	form.Set("csrf_token", csrfVal)

	// Execute 5 rapid failed attempts
	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: csrfVal})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("Attempt %d expected 401, got %d", i, rec.Code)
		}
	}

	// 6th attempt must trigger HTTP 429 Too Many Requests
	req6 := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req6.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req6.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: csrfVal})
	rec6 := httptest.NewRecorder()
	r.ServeHTTP(rec6, req6)

	if rec6.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected 429 Too Many Requests on 6th attempt, got %d", rec6.Code)
	}
}
