package services_test

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services"
	"mini-cdn/api/services/barrier"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/ssh_client"
)

func TestEndToEnd_FullMission1Lifecycle(t *testing.T) {
	fileRepo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	mockStorage := newMockSSHStorageWorker()
	defer mockStorage.close()

	geoSvc := geo.NewGeoService()
	barrierMutex := barrier.NewMutationBarrier()

	// Step 1: Pre-seed the 3 required physical storage servers (Singapore, US, London)
	sgNode := &models.StorageNode{
		ID:        "node-sg-id",
		NodeID:    "node-singapore",
		IP:        "sg",
		Port:      22,
		Region:    "Asia",
		Country:   "Singapore",
		Latitude:  1.3521,
		Longitude: 103.8198,
		Status:    models.NodeStatusOnline,
		CreatedAt: time.Now(),
	}
	usNode := &models.StorageNode{
		ID:        "node-us-id",
		NodeID:    "node-us",
		IP:        "us",
		Port:      22,
		Region:    "North America",
		Country:   "United States",
		Latitude:  40.7128,
		Longitude: -74.0060,
		Status:    models.NodeStatusOnline,
		CreatedAt: time.Now(),
	}
	lonNode := &models.StorageNode{
		ID:        "node-lon-id",
		NodeID:    "node-london",
		IP:        "london",
		Port:      22,
		Region:    "Europe",
		Country:   "United Kingdom",
		Latitude:  51.5074,
		Longitude: -0.1278,
		Status:    models.NodeStatusOnline,
		CreatedAt: time.Now(),
	}
	_ = nodeRepo.Create(context.Background(), sgNode)
	_ = nodeRepo.Create(context.Background(), usNode)
	_ = nodeRepo.Create(context.Background(), lonNode)

	syncSvc := services.NewSyncService(fileRepo, nodeRepo, mockStorage, barrierMutex)
	fileSvc := services.NewFileService(fileRepo, nodeRepo, mockStorage, geoSvc, barrierMutex, nil)

	// Step 2: Upload File & Verify Strict 3/3 Replication (COMPLETE)
	flagData := []byte("PNG_HEADER_MOCK_FLAG_IMAGE_BINARY_DATA_FOR_CDN_PREVIEW_TEST")
	fileMeta, repStatus, err := fileSvc.Upload(
		context.Background(),
		"flag.png",
		"image/png",
		bytes.NewReader(flagData),
		int64(len(flagData)),
	)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if repStatus.Success != true || fileMeta.ReplicationStatus != models.ReplicationStatusComplete {
		t.Fatalf("Expected 3/3 COMPLETE replication, got: %s (ReplicationStatus: %+v)", fileMeta.ReplicationStatus, repStatus)
	}
	if len(repStatus.SuccessfulReplicas) != 3 {
		t.Fatalf("Expected 3 successful replicas (Singapore, US, London), got: %d (%v)", len(repStatus.SuccessfulReplicas), repStatus.SuccessfulReplicas)
	}

	// Verify file is stored on all 3 storage nodes
	for _, n := range []*models.StorageNode{sgNode, usNode, lonNode} {
		sz, err := mockStorage.StatFile(context.Background(), n.IP, n.Port, fileMeta.ID)
		if err != nil || sz != int64(len(flagData)) {
			t.Fatalf("File missing or size mismatch on node %s (%s:%d): sz=%d, err=%v", n.NodeID, n.IP, n.Port, sz, err)
		}
	}

	// Step 3: GeoIP Proximity Routing & Fallback
	reqCambodia := httptest.NewRequest("GET", "/api/files/"+fileMeta.ID, nil)
	reqCambodia.Header.Set("X-Test-Country", "KH")

	stream, _, servedBy, err := fileSvc.GetFileStream(context.Background(), fileMeta.ID, reqCambodia)
	if err != nil {
		t.Fatalf("GetFileStream failed: %v", err)
	}
	readBytes, _ := io.ReadAll(stream)
	stream.Close()

	if !bytes.Equal(readBytes, flagData) {
		t.Fatalf("Downloaded image content mismatch")
	}
	if !strings.Contains(strings.ToLower(servedBy), "asia") && !strings.Contains(strings.ToLower(servedBy), "singapore") {
		t.Fatalf("Expected nearest node for Cambodia client to be Singapore/Asia, got: %s", servedBy)
	}

	// Simulate Singapore node failure -> fallback to London
	mockStorage.SetNodeFailing("sg", 22, true)
	streamFb, _, servedByFb, err := fileSvc.GetFileStream(context.Background(), fileMeta.ID, reqCambodia)
	if err != nil {
		t.Fatalf("Fallback failed when Singapore offline: %v", err)
	}
	streamFb.Close()
	if !strings.Contains(strings.ToLower(servedByFb), "europe") && !strings.Contains(strings.ToLower(servedByFb), "london") {
		t.Fatalf("Expected fallback to London, got: %s", servedByFb)
	}
	mockStorage.SetNodeFailing("sg", 22, false)

	// Step 4: Atomic Deletion across all replicas
	err = fileSvc.Delete(context.Background(), fileMeta.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deleted from all replicas
	for _, n := range []*models.StorageNode{sgNode, usNode, lonNode} {
		_, _, err := mockStorage.Download(context.Background(), n.IP, n.Port, fileMeta.ID)
		if err != ssh_client.ErrFileNotFound {
			t.Errorf("Expected ErrFileNotFound on node %s after delete, got %v", n.NodeID, err)
		}
	}

	// Step 5: Onboard New Storage Node (Tokyo) with concurrent mutations
	tokyoNode := &models.StorageNode{
		ID:        "node-tokyo-id",
		NodeID:    "node-tokyo",
		IP:        "tokyo",
		Port:      22,
		Region:    "Asia",
		Country:   "Japan",
		Latitude:  35.6762,
		Longitude: 139.6503,
		Status:    models.NodeStatusPending,
		CreatedAt: time.Now(),
	}
	_ = nodeRepo.Create(context.Background(), tokyoNode)

	// Upload new file before sync
	activeContent := "Active file that Tokyo must receive"
	activeMeta, _, err := fileSvc.Upload(context.Background(), "syllabus.txt", "text/plain", strings.NewReader(activeContent), int64(len(activeContent)))
	if err != nil {
		t.Fatalf("Upload before sync failed: %v", err)
	}

	var syncWg sync.WaitGroup
	syncWg.Add(1)
	var onboardErr error
	go func() {
		defer syncWg.Done()
		onboardErr = syncSvc.OnboardAndSynchronize(context.Background(), tokyoNode)
	}()

	syncWg.Wait()
	if onboardErr != nil {
		t.Fatalf("OnboardAndSynchronize failed: %v", onboardErr)
	}

	// Verify Tokyo is ONLINE
	updatedTokyo, err := nodeRepo.FindByNodeID(context.Background(), "node-tokyo")
	if err != nil || updatedTokyo.Status != models.NodeStatusOnline {
		t.Fatalf("Expected Tokyo to be ONLINE, got: %+v, err: %v", updatedTokyo, err)
	}

	// Verify active file was transferred to Tokyo
	sz, err := mockStorage.StatFile(context.Background(), "tokyo", 22, activeMeta.ID)
	if err != nil || sz != int64(len(activeContent)) {
		t.Fatalf("Active file missing on newly onboarded Tokyo node: sz=%d, err=%v", sz, err)
	}

	// Verify deleted file was NOT transferred (Zero Zombie Files)
	szDel, err := mockStorage.StatFile(context.Background(), "tokyo", 22, fileMeta.ID)
	if err != ssh_client.ErrFileNotFound && szDel > 0 {
		t.Fatalf("Zombie file detected on Tokyo: deleted file %s exists on disk", fileMeta.ID)
	}
}
