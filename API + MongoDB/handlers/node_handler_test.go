package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services"
)

func setupTestNodeHandler() (*NodeHandler, repository.NodeRepository, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	nodeRepo := repository.NewMockNodeRepository()
	syncService := services.NewSyncService()
	nodeService := services.NewNodeService(nodeRepo, syncService)
	handler := NewNodeHandler(nodeService)

	r := gin.New()
	r.GET("/api/nodes", handler.ListNodes)
	r.POST("/api/nodes", handler.OnboardNode)
	r.GET("/api/nodes/:id", handler.GetNode)
	r.DELETE("/api/nodes/:id", handler.RemoveNode)
	r.GET("/api/nodes/:id/health", handler.CheckNodeHealth)

	return handler, nodeRepo, r
}

func TestNodeHandler_ListNodesEmpty(t *testing.T) {
	_, _, r := setupTestNodeHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
}

func TestNodeHandler_GetAndRemoveNode(t *testing.T) {
	_, repo, r := setupTestNodeHandler()

	node := &models.StorageNode{
		ID:            "node-123",
		NodeID:        "node-singapore",
		IP:            "127.0.0.1",
		Port:          8081,
		Region:        "Asia",
		Country:       "Singapore",
		Latitude:      1.3521,
		Longitude:     103.8198,
		Status:        models.NodeStatusOnline,
		LastHeartbeat: time.Now(),
		CreatedAt:     time.Now(),
	}
	_ = repo.Create(t.Context(), node)

	// 1. Get Node
	getReq := httptest.NewRequest(http.MethodGet, "/api/nodes/node-singapore", nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 on GetNode, got %d", getRec.Code)
	}

	// 2. Remove Node
	delReq := httptest.NewRequest(http.MethodDelete, "/api/nodes/node-singapore", nil)
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 on RemoveNode, got %d", delRec.Code)
	}

	// 3. Get again -> 404
	getAgainReq := httptest.NewRequest(http.MethodGet, "/api/nodes/node-singapore", nil)
	getAgainRec := httptest.NewRecorder()
	r.ServeHTTP(getAgainRec, getAgainReq)

	if getAgainRec.Code != http.StatusNotFound {
		t.Fatalf("Expected 404 after remove, got %d", getAgainRec.Code)
	}
}

func TestNodeHandler_OnboardUnreachableNode(t *testing.T) {
	_, _, r := setupTestNodeHandler()

	payload := map[string]interface{}{
		"ip":     "198.51.100.254", // unreachable documentation IP
		"port":   9999,
		"region": "Asia",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/nodes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Must fail because worker is unreachable
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("Expected 502 Bad Gateway for unreachable node, got %d", rec.Code)
	}
}
