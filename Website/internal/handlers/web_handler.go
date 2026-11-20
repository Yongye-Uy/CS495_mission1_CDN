package handlers

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"mini-cdn/website/internal/config"
	"mini-cdn/website/internal/middleware"
)

type WebHandler struct {
	cfg         *config.Config
	apiTarget   *url.URL
	proxy       *httputil.ReverseProxy
	rateLimiter *middleware.RateLimiter
}

func NewWebHandler(cfg *config.Config) (*WebHandler, error) {
	apiTarget, err := url.Parse(cfg.ApiBaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid API_BASE_URL: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(apiTarget)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = apiTarget.Host
		if cfg.AdminAPIKey != "" {
			req.Header.Set("X-Admin-Key", cfg.AdminAPIKey)
		}
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[WEBSITE] Proxy error forwarding %s %s to %s: %v", r.Method, r.URL.Path, cfg.ApiBaseURL, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"success":false,"error":"Central API unreachable at %s: %v"}`, cfg.ApiBaseURL, err)))
	}

	return &WebHandler{
		cfg:         cfg,
		apiTarget:   apiTarget,
		proxy:       proxy,
		rateLimiter: middleware.NewRateLimiter(5, 1*time.Minute),
	}, nil
}

// HealthCheck handles GET /health
func (h *WebHandler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "website",
	})
}

// Index renders the main files dashboard
func (h *WebHandler) Index(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{
		"ApiBaseURL":   h.cfg.ApiBaseURL,
		"PublicApiURL": h.cfg.PublicApiURL,
		"ActiveNav":    "files",
	})
}

// NodesPage renders the storage nodes management table
func (h *WebHandler) NodesPage(c *gin.Context) {
	c.HTML(http.StatusOK, "nodes.html", gin.H{
		"ApiBaseURL":   h.cfg.ApiBaseURL,
		"PublicApiURL": h.cfg.PublicApiURL,
		"ActiveNav":    "nodes",
	})
}

// LoginPage renders the admin login page
func (h *WebHandler) LoginPage(c *gin.Context) {
	csrfToken, _ := c.Get("csrf_token")
	tokenStr, _ := csrfToken.(string)
	next := c.DefaultQuery("next", "/nodes/add")

	c.HTML(http.StatusOK, "login.html", gin.H{
		"CSRFToken": tokenStr,
		"Next":      next,
		"Error":     c.Query("error"),
	})
}

// HandleLogin processes credentials, checks rate limits, and issues session cookie
func (h *WebHandler) HandleLogin(c *gin.Context) {
	clientIP := c.ClientIP()
	if !h.rateLimiter.Allow(clientIP) {
		c.HTML(http.StatusTooManyRequests, "login.html", gin.H{
			"CSRFToken": c.GetString("csrf_token"),
			"Next":      c.PostForm("next"),
			"Error":     "Too many login attempts. Please wait 1 minute before trying again.",
		})
		return
	}

	password := c.PostForm("password")
	next := c.PostForm("next")
	if next == "" {
		next = "/nodes/add"
	}

	// Verify bcrypt password
	err := bcrypt.CompareHashAndPassword([]byte(h.cfg.AdminPasswordHash), []byte(password))
	if err != nil {
		c.HTML(http.StatusUnauthorized, "login.html", gin.H{
			"CSRFToken": c.GetString("csrf_token"),
			"Next":      next,
			"Error":     "Invalid admin credentials.",
		})
		return
	}

	// Login successful: reset rate limiter
	h.rateLimiter.Reset(clientIP)

	// Issue signed session token
	token := middleware.GenerateSessionToken(h.cfg.SessionSecret)
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(middleware.SessionCookieName, token, int(middleware.SessionTTL.Seconds()), "/", "", false, true)

	c.Redirect(http.StatusFound, next)
}

// HandleLogout clears the admin session cookie
func (h *WebHandler) HandleLogout(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(middleware.SessionCookieName, "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}

// AddNodePage renders the add storage node form (admin protected)
func (h *WebHandler) AddNodePage(c *gin.Context) {
	csrfToken, _ := c.Get("csrf_token")
	c.HTML(http.StatusOK, "add_node.html", gin.H{
		"ApiBaseURL":   h.cfg.ApiBaseURL,
		"PublicApiURL": h.cfg.PublicApiURL,
		"ActiveNav":    "add_node",
		"CSRFToken":    csrfToken,
	})
}

// HandleAddNode handles POST /nodes/add from form, injecting X-Admin-Key server-to-server
func (h *WebHandler) HandleAddNode(c *gin.Context) {
	targetURL := fmt.Sprintf("%s/api/nodes", strings.TrimRight(h.cfg.ApiBaseURL, "/"))

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Failed to read request body"})
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", c.GetHeader("Content-Type"))

	// Inject server-to-server Admin Key securely
	if h.cfg.AdminAPIKey != "" {
		req.Header.Set("X-Admin-Key", h.cfg.AdminAPIKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": fmt.Sprintf("Failed to contact Central API: %v", err)})
		return
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBytes)
}

// HandleUpload handles POST /upload
func (h *WebHandler) HandleUpload(c *gin.Context) {
	c.Request.URL.Path = "/api/files"
	h.proxy.ServeHTTP(c.Writer, c.Request)
}

// HandleDelete handles POST /delete/:id
func (h *WebHandler) HandleDelete(c *gin.Context) {
	id := c.Param("id")
	targetURL := fmt.Sprintf("%s/api/files/%s", strings.TrimRight(h.cfg.ApiBaseURL, "/"), id)

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodDelete, targetURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": fmt.Sprintf("Central API unreachable: %v", err)})
		return
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBytes)
}

// ProxyAPI forwards /api/* directly to Central API
func (h *WebHandler) ProxyAPI(c *gin.Context) {
	h.proxy.ServeHTTP(c.Writer, c.Request)
}
