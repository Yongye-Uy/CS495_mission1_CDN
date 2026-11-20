package services_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

func TestPhase3_ConcurrentMutationsDuringNodeSync(t *testing.T) {
	fileRepo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	journalRepo := repository.NewMockJournalRepository()
	syncJobRepo := repository.NewMockSyncJobRepository()
	mockStorage := newMockSSHStorageWorker()
	defer mockStorage.close()

	geoSvc := geo.NewGeoService()
	b := barrier.NewMutationBarrier()

	stagingDir, err := os.MkdirTemp("", "test_phase3_concurrency_*")
	if err != nil {
		t.Fatalf("Failed to create staging dir: %v", err)
	}
	defer os.RemoveAll(stagingDir)

	// Pre-seed two initial online nodes (sg, ny)
	sgNode := &models.StorageNode{
		ID:        "node-sg",
		NodeID:    "node-sg",
		IP:        "10.0.0.1",
		Port:      22,
		Region:    "Asia",
		Country:   "Singapore",
		Status:    models.NodeStatusOnline,
		CreatedAt: time.Now(),
	}
	nyNode := &models.StorageNode{
		ID:        "node-ny",
		NodeID:    "node-ny",
		IP:        "10.0.0.2",
		Port:      22,
		Region:    "US",
		Country:   "United States",
		Status:    models.NodeStatusOnline,
		CreatedAt: time.Now(),
	}
	_ = nodeRepo.Create(context.Background(), sgNode)
	_ = nodeRepo.Create(context.Background(), nyNode)

	syncSvc := services.NewSyncService(fileRepo, nodeRepo, journalRepo, syncJobRepo, mockStorage, b, stagingDir)
	fileSvc := services.NewFileService(fileRepo, nodeRepo, journalRepo, mockStorage, geoSvc, b, stagingDir, nil)

	// 1. Pre-upload 3 initial files
	for i := 1; i <= 3; i++ {
		content := fmt.Sprintf("Initial file content %d", i)
		name := fmt.Sprintf("pre_file_%d.txt", i)
		_, _, err := fileSvc.Upload(context.Background(), name, "text/plain", strings.NewReader(content), int64(len(content)))
		if err != nil {
			t.Fatalf("Initial upload failed: %v", err)
		}
	}

	// 2. Prepare new node (lon) in PENDING
	lonNode := &models.StorageNode{
		ID:        "node-lon",
		NodeID:    "node-lon",
		IP:        "10.0.0.3",
		Port:      22,
		Region:    "Europe",
		Country:   "United Kingdom",
		Status:    models.NodeStatusPending,
		CreatedAt: time.Now(),
	}
	_ = nodeRepo.Create(context.Background(), lonNode)

	// 3. Concurrently start node onboarding while performing uploads and deletes
	var wg sync.WaitGroup
	var syncErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		syncErr = syncSvc.OnboardAndSynchronize(context.Background(), lonNode)
	}()

	// Concurrent file operations during synchronization
	uploadDone := make(chan string, 5)
	for i := 1; i <= 5; i++ {
		go func(idx int) {
			content := fmt.Sprintf("Concurrent uploaded content %d", idx)
			meta, _, err := fileSvc.Upload(context.Background(), fmt.Sprintf("concurrent_%d.txt", idx), "text/plain", strings.NewReader(content), int64(len(content)))
			if err == nil && meta != nil {
				uploadDone <- meta.ID
			}
		}(i)
	}

	// Delete one of the initial files concurrently during sync
	initialFiles, _ := fileRepo.FindAll(context.Background())
	var deletedTargetID string
	if len(initialFiles) > 0 {
		deletedTargetID = initialFiles[0].ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(2 * time.Millisecond)
			_ = fileSvc.Delete(context.Background(), deletedTargetID)
		}()
	}

	wg.Wait()
	if syncErr != nil {
		t.Fatalf("SyncNewNode failed: %v", syncErr)
	}

	// 4. Verify node is now ONLINE
	updatedLon, err := nodeRepo.FindByNodeID(context.Background(), "node-lon")
	if err != nil || updatedLon.Status != models.NodeStatusOnline {
		t.Fatalf("Expected London node to be ONLINE, got: %+v, err: %v", updatedLon, err)
	}

	// 5. Verify Zero Zombie Files: Deleted files must NOT exist on London node
	if deletedTargetID != "" {
		statSize, err := mockStorage.StatFile(context.Background(), "10.0.0.3", 22, deletedTargetID)
		if err != ssh_client.ErrFileNotFound && statSize > 0 {
			t.Fatalf("Zombie file detected! Deleted file %s was found on newly onboarded node-lon", deletedTargetID)
		}
	}

	// 6. Verify Watermark Pruning
	onlineNodes, _ := nodeRepo.FindOnline(context.Background())
	if len(onlineNodes) != 3 {
		t.Fatalf("Expected 3 online nodes, found %d", len(onlineNodes))
	}
}

func TestPhase3_3Of3ReplicationAndStagingRetention(t *testing.T) {
	fileRepo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	journalRepo := repository.NewMockJournalRepository()
	mockStorage := newMockSSHStorageWorker()
	defer mockStorage.close()

	geoSvc := geo.NewGeoService()
	b := barrier.NewMutationBarrier()

	stagingDir, err := os.MkdirTemp("", "test_phase3_retention_*")
	if err != nil {
		t.Fatalf("Failed to create staging dir: %v", err)
	}
	defer os.RemoveAll(stagingDir)

	// Pre-seed all 3 online nodes in nodeRepo
	nodeSg := &models.StorageNode{ID: "node-sg", NodeID: "node-singapore", IP: "sg", Port: 22, Region: "Asia", Country: "Singapore", Status: models.NodeStatusOnline, CreatedAt: time.Now()}
	nodeNy := &models.StorageNode{ID: "node-ny", NodeID: "node-newyork", IP: "ny", Port: 22, Region: "US", Country: "United States", Status: models.NodeStatusOnline, CreatedAt: time.Now()}
	nodeLon := &models.StorageNode{ID: "node-lon", NodeID: "node-london", IP: "london", Port: 22, Region: "Europe", Country: "United Kingdom", Status: models.NodeStatusOnline, CreatedAt: time.Now()}
	_ = nodeRepo.Create(context.Background(), nodeSg)
	_ = nodeRepo.Create(context.Background(), nodeNy)
	_ = nodeRepo.Create(context.Background(), nodeLon)

	fileSvc := services.NewFileService(fileRepo, nodeRepo, journalRepo, mockStorage, geoSvc, b, stagingDir, nil)

	// Case A: 3/3 replication succeeds -> COMPLETE -> Staging file deleted
	content := "Data that replicates cleanly to all 3"
	metaA, repStatusA, err := fileSvc.Upload(context.Background(), "complete.txt", "text/plain", strings.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Upload A failed: %v", err)
	}
	if repStatusA.Success != true || metaA.ReplicationStatus != models.ReplicationStatusComplete {
		t.Fatalf("Expected ReplicationStatusComplete, got %s", metaA.ReplicationStatus)
	}
	// Verify local staging file removed
	if _, err := os.Stat(filepath.Join(stagingDir, metaA.ID)); !os.IsNotExist(err) {
		t.Errorf("Staging file for COMPLETE replication must be deleted, but still exists on disk")
	}

	// Case B: 2/3 replication (one node failing) -> PARTIAL -> Staging file RETAINED
	mockStorage.SetNodeFailing("london", 22, true)
	contentB := "Data with one node failure"
	metaB, repStatusB, err := fileSvc.Upload(context.Background(), "partial.txt", "text/plain", strings.NewReader(contentB), int64(len(contentB)))
	if err != nil {
		t.Fatalf("Upload B failed: %v", err)
	}
	if repStatusB.Success != false || metaB.ReplicationStatus != models.ReplicationStatusPartial {
		t.Fatalf("Expected ReplicationStatusPartial, got %s", metaB.ReplicationStatus)
	}
	if metaB.StagedPath == "" {
		t.Errorf("Expected StagedPath to be set for PARTIAL replication")
	}
	if _, err := os.Stat(metaB.StagedPath); os.IsNotExist(err) {
		t.Errorf("Staging file for PARTIAL replication MUST be retained on disk, but was missing")
	}

	// Case C: Recovery worker heals the node and cleans the staging file
	mockStorage.SetNodeFailing("london", 22, false)
	worker := services.NewReplicationRecoveryWorker(fileRepo, nodeRepo, mockStorage, b, stagingDir, 100*time.Millisecond)

	// Run single iteration
	worker.RunRecoveryCycle(context.Background())

	// Verify status updated to COMPLETE in repo
	updatedB, err := fileRepo.FindByID(context.Background(), metaB.ID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if updatedB.ReplicationStatus != models.ReplicationStatusComplete {
		t.Errorf("Expected RecoveryWorker to elevate status to COMPLETE, got %s", updatedB.ReplicationStatus)
	}
	// Verify staging file removed after recovery
	if _, err := os.Stat(metaB.StagedPath); !os.IsNotExist(err) {
		t.Errorf("Staging file should be deleted after successful recovery, but still exists")
	}
}
