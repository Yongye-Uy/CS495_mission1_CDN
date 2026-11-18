package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mini-cdn/website/internal/config"
)

func createTestTemplates(t *testing.T) string {
	tempDir, err := os.MkdirTemp("", "tmpl_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	indexContent := `<!DOCTYPE html><html><body><h1>Mini CDN Test</h1></body></html>`
	if err := os.WriteFile(filepath.Join(tempDir, "index.html"), []byte(indexContent), 0644); err != nil {
		t.Fatalf("Failed to write index.html: %v", err)
	}

	nodesContent := `<!DOCTYPE html><html><body><h1>Nodes Test</h1></body></html>`
	if err := os.WriteFile(filepath.Join(tempDir, "nodes.html"), []byte(nodesContent), 0644); err != nil {
		t.Fatalf("Failed to write nodes.html: %v", err)
	}

	addNodeContent := `<!DOCTYPE html><html><body><h1>Add Node Test</h1></body></html>`
	if err := os.WriteFile(filepath.Join(tempDir, "add_node.html"), []byte(addNodeContent), 0644); err != nil {
		t.Fatalf("Failed to write add_node.html: %v", err)
	}

	return tempDir
}

func setupTestEngine(t *testing.T, apiURL string) (*gin.Engine, string) {
	gin.SetMode(gin.TestMode)
	tmplDir := createTestTemplates(t)

	cfg := &config.Config{
		Port:       "3000",
		ApiBaseURL: apiURL,
	}

	handler, err := NewWebHandler(cfg)
	if err != nil {
		t.Fatalf("NewWebHandler failed: %v", err)
	}

	r := gin.New()
	r.LoadHTMLGlob(filepath.Join(tmplDir, "*.html"))

	r.GET("/health", handler.HealthCheck)
	r.GET("/", handler.Index)
	r.GET("/files", handler.Index)
	r.GET("/nodes", handler.NodesPage)
	r.GET("/nodes/add", handler.AddNodePage)
	r.Any("/api/*path", handler.ProxyAPI)

	return r, tmplDir
}

func TestWebHandler_HealthCheck(t *testing.T) {
	r, tmplDir := setupTestEngine(t, "http://localhost:8080")
	defer os.RemoveAll(tmplDir)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var data map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if data["status"] != "ok" || data["service"] != "website" {
		t.Errorf("Unexpected health data: %+v", data)
	}
}

func TestWebHandler_Pages(t *testing.T) {
	r, tmplDir := setupTestEngine(t, "http://localhost:8080")
	defer os.RemoveAll(tmplDir)

	// Test Index / Files
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Mini CDN Test") {
		t.Errorf("Expected Index page to contain 'Mini CDN Test', got %s", rec.Body.String())
	}

	// Test Nodes page
	reqNodes := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	recNodes := httptest.NewRecorder()
	r.ServeHTTP(recNodes, reqNodes)
	if recNodes.Code != http.StatusOK || !strings.Contains(recNodes.Body.String(), "Nodes Test") {
		t.Errorf("Expected Nodes page to contain 'Nodes Test', got %s", recNodes.Body.String())
	}

	// Test Add Node page
	reqAdd := httptest.NewRequest(http.MethodGet, "/nodes/add", nil)
	recAdd := httptest.NewRecorder()
	r.ServeHTTP(recAdd, reqAdd)
	if recAdd.Code != http.StatusOK || !strings.Contains(recAdd.Body.String(), "Add Node Test") {
		t.Errorf("Expected Add Node page to contain 'Add Node Test', got %s", recAdd.Body.String())
	}
}

type closeNotifyingRecorder struct {
	*httptest.ResponseRecorder
	closed chan bool
}

func newCloseNotifyingRecorder() *closeNotifyingRecorder {
	return &closeNotifyingRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closed:           make(chan bool, 1),
	}
}

func (c *closeNotifyingRecorder) CloseNotify() <-chan bool {
	return c.closed
}

func TestWebHandler_Proxy(t *testing.T) {
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"mocked": true, "path": "` + r.URL.Path + `"}`))
	}))
	defer mockAPI.Close()

	r, tmplDir := setupTestEngine(t, mockAPI.URL)
	defer os.RemoveAll(tmplDir)

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	rec := newCloseNotifyingRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200 from proxy, got %d", rec.Code)
	}

	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("Failed to decode proxy response: %v", err)
	}

	if res["mocked"] != true {
		t.Errorf("Expected proxy to return mocked: true, got %+v", res)
	}
}
