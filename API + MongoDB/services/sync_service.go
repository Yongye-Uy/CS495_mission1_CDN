package services

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services/barrier"
	"mini-cdn/api/services/ssh_client"
)

type SyncService struct {
	fileRepo      repository.FileRepository
	nodeRepo      repository.NodeRepository
	storageClient ssh_client.StorageClient
	barrier       *barrier.MutationBarrier
}

func NewSyncService(
	fileRepo repository.FileRepository,
	nodeRepo repository.NodeRepository,
	storageClient ssh_client.StorageClient,
	barrier *barrier.MutationBarrier,
) *SyncService {
	return &SyncService{
		fileRepo:      fileRepo,
		nodeRepo:      nodeRepo,
		storageClient: storageClient,
		barrier:       barrier,
	}
}

// OnboardAndSynchronize executes the catalog-driven synchronization protocol,
// audits file catalog convergence under an exclusive in-memory mutation barrier,
// and activates the storage node to ONLINE.
func (s *SyncService) OnboardAndSynchronize(ctx context.Context, targetNode *models.StorageNode) error {
	if targetNode == nil {
		return fmt.Errorf("target storage node is nil")
	}
	if s.storageClient == nil {
		return fmt.Errorf("SSH storage client is uninitialized on Central API server")
	}

	port := targetNode.Port
	if port <= 0 {
		port = 22
	}

	log.Printf("[SYNC] Starting onboarding validation for %s (%s:%d)...", targetNode.NodeID, targetNode.IP, port)

	// Step 1: Strict SSH and storage directory accessibility check
	healthy, err := s.storageClient.Health(ctx, targetNode.IP, port)
	if err != nil || !healthy {
		return fmt.Errorf("storage node %s (%s:%d) health check failed: %w", targetNode.NodeID, targetNode.IP, port, err)
	}

	// Step 2: Mark node status as SYNCING in MongoDB
	_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusSyncing)

	// Step 3: Phase 1 — Catalog-Driven Synchronization
	log.Printf("[SYNC] Phase 1: Snapshot sync initiated for %s", targetNode.NodeID)
	activeFiles, err := s.fileRepo.FindActive(ctx)
	if err != nil {
		_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusError)
		return fmt.Errorf("failed to query active files: %w", err)
	}

	syncedCount := 0
	for _, file := range activeFiles {
		if file.IsDeleted {
			continue
		}
		if err := s.transferFileToTarget(ctx, file, targetNode); err != nil {
			failMsg := fmt.Sprintf("Missing source replica / transfer error for file %s (%s): %v", file.ID, file.OriginalName, err)
			log.Printf("[SYNC] %s. Aborting onboarding activation.", failMsg)
			_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusError)
			return fmt.Errorf("%s", failMsg)
		}
		syncedCount++
	}

	log.Printf("[SYNC] Phase 1 completed: %d files transferred to %s.", syncedCount, targetNode.NodeID)

	// Step 4: Phase 2 — Final Reconciliation Barrier & Activation
	log.Printf("[SYNC] Entering final mutation barrier for node %s...", targetNode.NodeID)
	s.barrier.Lock()
	defer s.barrier.Unlock()

	// Re-query active files under exclusive barrier lock to catch any concurrent mutations
	latestActiveFiles, err := s.fileRepo.FindActive(ctx)
	if err != nil {
		_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusError)
		return fmt.Errorf("failed to query active files during final barrier: %w", err)
	}

	activeFilesMap := make(map[string]*models.FileMetadata)
	for _, f := range latestActiveFiles {
		if !f.IsDeleted {
			activeFilesMap[f.ID] = f
		}
	}

	// Audit remote filesystem
	remoteFiles, err := s.storageClient.ListFiles(ctx, targetNode.IP, port)
	if err != nil {
		failMsg := fmt.Sprintf("Final SFTP directory audit failed on %s: %v", targetNode.NodeID, err)
		_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusError)
		return fmt.Errorf("%s", failMsg)
	}

	diskFilesMap := make(map[string]bool)
	for _, f := range remoteFiles {
		diskFilesMap[f] = true
	}

	// 1. Delete any files on target disk that were deleted or are untracked
	for diskFile := range diskFilesMap {
		if _, ok := activeFilesMap[diskFile]; !ok {
			log.Printf("[SYNC] Reconciling: removing deleted/untracked file %s from %s", diskFile, targetNode.NodeID)
			_ = s.storageClient.Delete(ctx, targetNode.IP, port, diskFile)
		}
	}

	// 2. Transfer any active files missing from target disk
	for fileID, file := range activeFilesMap {
		if !diskFilesMap[fileID] {
			log.Printf("[SYNC] Reconciling: transferring missing active file %s to %s", fileID, targetNode.NodeID)
			if err := s.transferFileToTarget(ctx, file, targetNode); err != nil {
				failMsg := fmt.Sprintf("Audit failure: active file %s (%s) could not be synced to %s: %v", file.ID, file.OriginalName, targetNode.NodeID, err)
				_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusError)
				return fmt.Errorf("%s", failMsg)
			}
		}
	}

	// Final verification of remote files
	finalRemoteFiles, err := s.storageClient.ListFiles(ctx, targetNode.IP, port)
	if err == nil {
		finalMap := make(map[string]bool)
		for _, f := range finalRemoteFiles {
			finalMap[f] = true
		}
		for fileID, f := range activeFilesMap {
			if !finalMap[fileID] {
				failMsg := fmt.Sprintf("Final verification failure: file %s (%s) missing on %s", f.ID, f.OriginalName, targetNode.NodeID)
				_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusError)
				return fmt.Errorf("%s", failMsg)
			}
		}
	}

	// Update MongoDB metadata atomically
	newReplica := models.Replica{
		Location: strings.ToLower(targetNode.Region),
		URL:      targetNode.NodeID,
	}
	_ = s.fileRepo.AddReplicaToActiveFiles(ctx, newReplica)
	_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusOnline)

	log.Printf("[SYNC] Barrier release! Node %s successfully onboarded and activated to ONLINE.", targetNode.NodeID)
	return nil
}

func (s *SyncService) transferFileToTarget(ctx context.Context, file *models.FileMetadata, targetNode *models.StorageNode) error {
	targetPort := targetNode.Port
	if targetPort <= 0 {
		targetPort = 22
	}

	// Stream from an online healthy replica holding the file
	for _, r := range file.Replicas {
		sourceNode, err := s.nodeRepo.FindByNodeID(ctx, r.URL)
		if err == nil && sourceNode != nil && sourceNode.Status == models.NodeStatusOnline {
			sourcePort := sourceNode.Port
			if sourcePort <= 0 {
				sourcePort = 22
			}
			stream, _, err := s.storageClient.Download(ctx, sourceNode.IP, sourcePort, file.ID)
			if err == nil {
				// Ephemeral temp file on API
				tempFile, err := os.CreateTemp("", "cdn-sync-*")
				if err != nil {
					stream.Close()
					continue
				}
				tempPath := tempFile.Name()

				_, copyErr := io.Copy(tempFile, stream)
				_ = tempFile.Close()
				_ = stream.Close()

				if copyErr != nil {
					_ = os.Remove(tempPath)
					continue
				}

				uploadErr := s.storageClient.Upload(ctx, targetNode.IP, targetPort, file.ID, tempPath)
				_ = os.Remove(tempPath)
				if uploadErr == nil {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("no available healthy source replica found for file %s", file.ID)
}
