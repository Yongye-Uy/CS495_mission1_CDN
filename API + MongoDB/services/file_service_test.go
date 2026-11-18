package services

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/storage_client"
)

// MockStorageClient implements storage_client.StorageClient for unit tests.
type MockStorageClient struct {
	mu           sync.Mutex
	storedFiles  map[string]map[string][]byte // nodeURL -> fileID -> content
	failingNodes map[string]bool              // nodeURL -> should fail
}

func NewMockStorageClient() *MockStorageClient {
	return &MockStorageClient{
		storedFiles:  make(map[string]map[string][]byte),
		failingNodes: make(map[string]bool),
	}
}

func (m *MockStorageClient) SetNodeFailing(nodeURL string, failing bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failingNodes[nodeURL] = failing
}

func (m *MockStorageClient) Upload(ctx context.Context, nodeURL string, fileID string, filename string, r io.Reader) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failingNodes[nodeURL] {
		return errors.New("simulated storage failure")
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if _, exists := m.storedFiles[nodeURL]; !exists {
		m.storedFiles[nodeURL] = make(map[string][]byte)
	}
	m.storedFiles[nodeURL][fileID] = data
	return nil
}

func (m *MockStorageClient) Download(ctx context.Context, nodeURL string, fileID string) (io.ReadCloser, int64, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failingNodes[nodeURL] {
		return nil, 0, "", errors.New("simulated storage failure")
	}

	nodeFiles, exists := m.storedFiles[nodeURL]
	if !exists {
		return nil, 0, "", storage_client.ErrStorageNotFound
	}

	data, exists := nodeFiles[fileID]
	if !exists {
		return nil, 0, "", storage_client.ErrStorageNotFound
	}

	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), "text/plain", nil
}

func (m *MockStorageClient) Delete(ctx context.Context, nodeURL string, fileID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failingNodes[nodeURL] {
		return errors.New("simulated storage failure")
	}

	if nodeFiles, exists := m.storedFiles[nodeURL]; exists {
		delete(nodeFiles, fileID)
	}
	return nil
}

func (m *MockStorageClient) Health(ctx context.Context, nodeURL string) (bool, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failingNodes[nodeURL] {
		return false, "", errors.New("node offline")
	}
	return true, "mock-loc", nil
}

func TestFileService_UploadSingleNode(t *testing.T) {
	repo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	client := NewMockStorageClient()
	geoSvc := geo.NewGeoService()

	nodes := []models.Replica{
		{Location: "singapore", URL: "http://storage:8081"},
	}

	svc := NewFileService(repo, nodeRepo, client, geoSvc, nodes)

	content := "Hello single node storage"
	meta, repStatus, err := svc.Upload(context.Background(), "test.txt", "text/plain", strings.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if !repStatus.Success {
		t.Errorf("Expected replication success, got %v", repStatus)
	}
	if len(repStatus.SuccessfulReplicas) != 1 || repStatus.SuccessfulReplicas[0] != "singapore" {
		t.Errorf("Unexpected successful replicas: %+v", repStatus.SuccessfulReplicas)
	}

	// Verify MongoDB persistence
	saved, err := repo.FindByID(context.Background(), meta.ID)
	if err != nil {
		t.Fatalf("Failed to find file in repo: %v", err)
	}
	if saved.OriginalName != "test.txt" || saved.Size != int64(len(content)) {
		t.Errorf("Saved metadata mismatch: %+v", saved)
	}
}

func TestFileService_ReplicationStatusMultiNode(t *testing.T) {
	repo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	client := NewMockStorageClient()
	geoSvc := geo.NewGeoService()

	nodes := []models.Replica{
		{Location: "singapore", URL: "http://sg:8081"},
		{Location: "newyork", URL: "http://ny:8081"},
		{Location: "london", URL: "http://london:8081"},
	}

	// Simulate London failure
	client.SetNodeFailing("http://london:8081", true)

	svc := NewFileService(repo, nodeRepo, client, geoSvc, nodes)

	content := "Replication test data"
	meta, repStatus, err := svc.Upload(context.Background(), "multi.pdf", "application/pdf", strings.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Upload should not have returned error for partial failure: %v", err)
	}

	if repStatus.Success {
		t.Errorf("Expected repStatus.Success to be false due to failed London node")
	}

	if len(repStatus.FailedReplicas) != 1 || repStatus.FailedReplicas[0] != "london" {
		t.Errorf("Expected failed replica 'london', got %+v", repStatus.FailedReplicas)
	}

	if len(repStatus.SuccessfulReplicas) != 2 {
		t.Errorf("Expected 2 successful replicas, got %+v", repStatus.SuccessfulReplicas)
	}

	// Check metadata stored only successful replicas
	saved, err := repo.FindByID(context.Background(), meta.ID)
	if err != nil {
		t.Fatalf("Metadata should be stored: %v", err)
	}
	if len(saved.Replicas) != 2 {
		t.Errorf("Expected 2 saved replicas in metadata, got %d", len(saved.Replicas))
	}
}

func TestFileService_NearestServerAndFallback(t *testing.T) {
	repo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	client := NewMockStorageClient()
	geoSvc := geo.NewGeoService()

	nodes := []models.Replica{
		{Location: "singapore", URL: "http://sg:8081"},
		{Location: "newyork", URL: "http://ny:8081"},
		{Location: "london", URL: "http://london:8081"},
	}

	svc := NewFileService(repo, nodeRepo, client, geoSvc, nodes)

	content := "Test content for geolocation download"
	meta, _, err := svc.Upload(context.Background(), "geo.txt", "text/plain", strings.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// 1. Client from Cambodia (KH) -> nearest is Singapore
	reqKH := httptest.NewRequest("GET", "/api/files/"+meta.ID, nil)
	reqKH.Header.Set("X-Test-Country", "KH")

	stream, _, servedBy, err := svc.GetFileStream(context.Background(), meta.ID, reqKH)
	if err != nil {
		t.Fatalf("GetFileStream failed: %v", err)
	}
	stream.Close()

	if servedBy != "singapore" {
		t.Errorf("Expected served by singapore, got %s", servedBy)
	}

	// 2. Simulate Singapore node failure
	client.SetNodeFailing("http://sg:8081", true)

	// Now Cambodia client tries again -> Singapore fails, fallback to next nearest (London ~9970km vs NY ~15000km)
	streamFallback, _, servedByFallback, err := svc.GetFileStream(context.Background(), meta.ID, reqKH)
	if err != nil {
		t.Fatalf("GetFileStream should succeed via fallback: %v", err)
	}
	streamFallback.Close()

	if servedByFallback != "london" {
		t.Errorf("Expected fallback served by london, got %s", servedByFallback)
	}
}

func TestFileService_Delete(t *testing.T) {
	repo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	client := NewMockStorageClient()
	geoSvc := geo.NewGeoService()

	nodes := []models.Replica{
		{Location: "singapore", URL: "http://sg:8081"},
		{Location: "newyork", URL: "http://ny:8081"},
	}

	svc := NewFileService(repo, nodeRepo, client, geoSvc, nodes)

	content := "Delete target"
	meta, _, err := svc.Upload(context.Background(), "del.txt", "text/plain", strings.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// Delete file
	err = svc.Delete(context.Background(), meta.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify metadata removed
	_, err = repo.FindByID(context.Background(), meta.ID)
	if err != repository.ErrNotFound {
		t.Errorf("Expected ErrNotFound after deletion, got %v", err)
	}
}
