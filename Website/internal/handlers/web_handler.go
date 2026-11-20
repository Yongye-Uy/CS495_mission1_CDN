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

	"github.com/gin-gonic/gin"

	"mini-cdn/website/internal/config"
)

type WebHandler struct {
	cfg       *config.Config
	apiTarget *url.URL
	proxy     *httputil.ReverseProxy
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
		if cfg.CentralApiAdminKey != "" {
			req.Header.Set("X-Admin-Key", cfg.CentralApiAdminKey)
		}
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[WEBSITE] Proxy error forwarding %s %s to %s: %v", r.Method, r.URL.Path, cfg.ApiBaseURL, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"success":false,"error":"Central API unreachable at %s: %v"}`, cfg.ApiBaseURL, err)))
	}

	return &WebHandler{
		cfg:       cfg,
		apiTarget: apiTarget,
		proxy:     proxy,
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

// AddNodePage renders the add storage node form (direct access)
func (h *WebHandler) AddNodePage(c *gin.Context) {
	c.HTML(http.StatusOK, "add_node.html", gin.H{
		"ApiBaseURL":   h.cfg.ApiBaseURL,
		"PublicApiURL": h.cfg.PublicApiURL,
		"ActiveNav":    "add_node",
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
	if h.cfg.CentralApiAdminKey != "" {
		req.Header.Set("X-Admin-Key", h.cfg.CentralApiAdminKey)
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
