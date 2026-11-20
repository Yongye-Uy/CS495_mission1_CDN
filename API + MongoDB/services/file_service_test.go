package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services/barrier"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/ssh_client"
)

// MockStorageClient implements ssh_client.StorageClient for unit tests.
type MockStorageClient struct {
	mu           sync.Mutex
	storedFiles  map[string]map[string][]byte // "host:port" -> fileID -> content
	failingNodes map[string]bool              // "host:port" -> should fail
}

func NewMockStorageClient() *MockStorageClient {
	return &MockStorageClient{
		storedFiles:  make(map[string]map[string][]byte),
		failingNodes: make(map[string]bool),
	}
}

func (m *MockStorageClient) key(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

func (m *MockStorageClient) SetNodeFailing(host string, port int, failing bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failingNodes[m.key(host, port)] = failing
}

func (m *MockStorageClient) AutoEnrollHostKey(ctx context.Context, host string, port int) error {
	return nil
}

func (m *MockStorageClient) Upload(ctx context.Context, host string, port int, fileID string, localFilePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(host, port)
	if m.failingNodes[k] {
		return errors.New("simulated storage failure")
	}

	data, err := os.ReadFile(localFilePath)
	if err != nil {
		return err
	}

	if _, exists := m.storedFiles[k]; !exists {
		m.storedFiles[k] = make(map[string][]byte)
	}
	m.storedFiles[k][fileID] = data
	return nil
}

func (m *MockStorageClient) Download(ctx context.Context, host string, port int, fileID string) (io.ReadCloser, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(host, port)
	if m.failingNodes[k] {
		return nil, 0, errors.New("simulated storage failure")
	}

	nodeFiles, exists := m.storedFiles[k]
	if !exists {
		return nil, 0, ssh_client.ErrFileNotFound
	}

	data, exists := nodeFiles[fileID]
	if !exists {
		return nil, 0, ssh_client.ErrFileNotFound
	}

	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (m *MockStorageClient) Delete(ctx context.Context, host string, port int, fileID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(host, port)
	if m.failingNodes[k] {
		return errors.New("simulated storage failure")
	}

	if nodeFiles, exists := m.storedFiles[k]; exists {
		delete(nodeFiles, fileID)
	}
	return nil
}

func (m *MockStorageClient) Health(ctx context.Context, host string, port int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(host, port)
	if m.failingNodes[k] {
		return false, errors.New("node offline")
	}
	return true, nil
}

func (m *MockStorageClient) ListFiles(ctx context.Context, host string, port int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(host, port)
	var list []string
	if nodeFiles, exists := m.storedFiles[k]; exists {
		for f := range nodeFiles {
			list = append(list, f)
		}
	}
	return list, nil
}

func (m *MockStorageClient) StatFile(ctx context.Context, host string, port int, fileID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := m.key(host, port)
	if nodeFiles, exists := m.storedFiles[k]; exists {
		if data, ok := nodeFiles[fileID]; ok {
			return int64(len(data)), nil
		}
	}
	return 0, ssh_client.ErrFileNotFound
}

func TestFileService_Upload3of3Complete(t *testing.T) {
	repo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	client := NewMockStorageClient()
	geoSvc := geo.NewGeoService()
	b := barrier.NewMutationBarrier()

	nodes := []models.Replica{
		{Location: "singapore", URL: "sg:22"},
		{Location: "newyork", URL: "ny:22"},
		{Location: "london", URL: "london:22"},
	}

	svc := NewFileService(repo, nodeRepo, client, geoSvc, b, nodes)

	content := "Hello 3/3 complete replication test"
	meta, repStatus, err := svc.Upload(context.Background(), "test.txt", "text/plain", strings.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// Verify status is COMPLETE when 3/3 replicas succeed
	if repStatus.Success != true {
		t.Errorf("Expected repStatus.Success to be true for 3/3, got false")
	}
	if meta.ReplicationStatus != models.ReplicationStatusComplete {
		t.Errorf("Expected status COMPLETE, got %s", meta.ReplicationStatus)
	}
	if len(repStatus.SuccessfulReplicas) != 3 {
		t.Errorf("Expected 3 successful replicas, got %d", len(repStatus.SuccessfulReplicas))
	}
}

func TestFileService_ReplicationStatusPartial(t *testing.T) {
	repo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	client := NewMockStorageClient()
	geoSvc := geo.NewGeoService()
	b := barrier.NewMutationBarrier()

	nodes := []models.Replica{
		{Location: "singapore", URL: "sg:22"},
		{Location: "newyork", URL: "ny:22"},
		{Location: "london", URL: "london:22"},
	}

	// Simulate London failure
	client.SetNodeFailing("london", 22, true)

	svc := NewFileService(repo, nodeRepo, client, geoSvc, b, nodes)

	content := "Partial replication data"
	meta, repStatus, err := svc.Upload(context.Background(), "partial.pdf", "application/pdf", strings.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Upload should succeed with PARTIAL status, got: %v", err)
	}

	// 2/3 is PARTIAL, not complete!
	if repStatus.Success {
		t.Errorf("Expected repStatus.Success to be false due to failed London node")
	}
	if meta.ReplicationStatus != models.ReplicationStatusPartial {
		t.Errorf("Expected status PARTIAL, got %s", meta.ReplicationStatus)
	}
	if !meta.QuorumMet {
		t.Errorf("Expected QuorumMet to be true for 2/3 replicas")
	}
	if len(meta.Replicas) != 2 {
		t.Errorf("Expected exactly 2 replicas recorded in metadata, got %d", len(meta.Replicas))
	}
}

func TestFileService_NearestServerAndFallback(t *testing.T) {
	repo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	client := NewMockStorageClient()
	geoSvc := geo.NewGeoService()
	b := barrier.NewMutationBarrier()

	nodes := []models.Replica{
		{Location: "singapore", URL: "sg:22"},
		{Location: "newyork", URL: "ny:22"},
		{Location: "london", URL: "london:22"},
	}

	svc := NewFileService(repo, nodeRepo, client, geoSvc, b, nodes)

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

	// 2. Simulate Singapore node failure -> fallback to London (~9970km vs NY ~15000km)
	client.SetNodeFailing("sg", 22, true)

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
	b := barrier.NewMutationBarrier()

	nodes := []models.Replica{
		{Location: "singapore", URL: "sg:22"},
		{Location: "newyork", URL: "ny:22"},
		{Location: "london", URL: "london:22"},
	}

	svc := NewFileService(repo, nodeRepo, client, geoSvc, b, nodes)

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

	// Verify metadata marked as deleted
	_, err = repo.FindByID(context.Background(), meta.ID)
	if err != repository.ErrNotFound {
		t.Errorf("Expected ErrNotFound after deletion, got %v", err)
	}

	// Verify deleted from remote storage nodes
	_, _, err = client.Download(context.Background(), "sg", 22, meta.ID)
	if err != ssh_client.ErrFileNotFound {
		t.Errorf("Expected ErrFileNotFound on node after SFTP delete, got: %v", err)
	}
}
