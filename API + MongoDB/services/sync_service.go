package services

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services/barrier"
	"mini-cdn/api/services/ssh_client"
)

type SyncService struct {
	fileRepo      repository.FileRepository
	nodeRepo      repository.NodeRepository
	journalRepo   repository.JournalRepository
	syncJobRepo   repository.SyncJobRepository
	storageClient ssh_client.StorageClient
	barrier       *barrier.MutationBarrier
	stagingDir    string
}

func NewSyncService(
	fileRepo repository.FileRepository,
	nodeRepo repository.NodeRepository,
	journalRepo repository.JournalRepository,
	syncJobRepo repository.SyncJobRepository,
	storageClient ssh_client.StorageClient,
	barrier *barrier.MutationBarrier,
	stagingDir string,
) *SyncService {
	if stagingDir == "" {
		stagingDir = filepath.Join(os.TempDir(), "mission1_staging")
	}
	return &SyncService{
		fileRepo:      fileRepo,
		nodeRepo:      nodeRepo,
		journalRepo:   journalRepo,
		syncJobRepo:   syncJobRepo,
		storageClient: storageClient,
		barrier:       barrier,
		stagingDir:    stagingDir,
	}
}

// OnboardAndSynchronize executes the full two-phase mutation journal synchronization protocol,
// audits file catalog convergence, and activates the storage node to ONLINE under the mutation barrier.
func (s *SyncService) OnboardAndSynchronize(ctx context.Context, targetNode *models.StorageNode) error {
	if targetNode == nil {
		return fmt.Errorf("target storage node is nil")
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

	// Step 2: Initialize persistent sync job in MongoDB
	startSeq, err := s.journalRepo.GetMaxSequence(ctx)
	if err != nil {
		return fmt.Errorf("failed to retrieve current journal sequence: %w", err)
	}

	job := &models.SyncJob{
		NodeID:          targetNode.NodeID,
		Status:          models.SyncStatusSyncing,
		StartSeq:        startSeq,
		LastReplayedSeq: startSeq,
		StartedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.syncJobRepo.CreateJob(ctx, job); err != nil {
		return fmt.Errorf("failed to initialize persistent sync job: %w", err)
	}

	_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusSyncing)

	// Step 3: Phase 1 — Catalog-Driven Snapshot Synchronization
	log.Printf("[SYNC] Phase 1: Snapshot sync initiated for %s from startSeq=%d", targetNode.NodeID, startSeq)
	activeFiles, err := s.fileRepo.FindActive(ctx)
	if err != nil {
		_ = s.syncJobRepo.FailJob(ctx, targetNode.NodeID, err.Error())
		return fmt.Errorf("failed to query active files: %w", err)
	}

	job.TotalFiles = len(activeFiles)

	for _, file := range activeFiles {
		if file.IsDeleted {
			continue
		}
		if err := s.transferFileToTarget(ctx, file, targetNode); err != nil {
			failMsg := fmt.Sprintf("Missing source replica / transfer error for file %s (%s): %v", file.ID, file.OriginalName, err)
			log.Printf("[SYNC] %s. Aborting onboarding activation.", failMsg)
			_ = s.syncJobRepo.FailJob(ctx, targetNode.NodeID, failMsg)
			_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusError)
			return fmt.Errorf("%s", failMsg)
		}
		job.SyncedFiles++
		_ = s.syncJobRepo.UpdateJobProgress(ctx, targetNode.NodeID, job.LastReplayedSeq, job.SyncedFiles)
	}

	log.Printf("[SYNC] Phase 1 completed: %d files transferred to %s.", job.SyncedFiles, targetNode.NodeID)

	// Step 4: Phase 2 — Ordered Journal Replay Loop
	log.Printf("[SYNC] Phase 2: Catching up mutation journal records after seq %d...", job.LastReplayedSeq)
	for {
		entries, err := s.journalRepo.GetMutationsAfter(ctx, job.LastReplayedSeq)
		if err != nil {
			_ = s.syncJobRepo.FailJob(ctx, targetNode.NodeID, err.Error())
			return fmt.Errorf("failed to query mutations after seq %d: %w", job.LastReplayedSeq, err)
		}

		if len(entries) <= 5 {
			log.Printf("[SYNC] Journal backlog down to %d entries. Proceeding to final activation barrier.", len(entries))
			break
		}

		for _, entry := range entries {
			if err := s.applyJournalMutation(ctx, entry, targetNode); err != nil {
				log.Printf("[SYNC] Warning: error applying journal mutation seq=%d to %s: %v", entry.Seq, targetNode.NodeID, err)
			}
			job.LastReplayedSeq = entry.Seq
		}
		_ = s.syncJobRepo.UpdateJobProgress(ctx, targetNode.NodeID, job.LastReplayedSeq, job.SyncedFiles)
	}

	// Step 5: Final Mutation Barrier Phase
	log.Printf("[SYNC] Entering final mutation barrier for node %s...", targetNode.NodeID)
	s.barrier.Lock()
	defer s.barrier.Unlock()

	// Replay final remaining journal tail under exclusive barrier lock
	tailEntries, err := s.journalRepo.GetMutationsAfter(ctx, job.LastReplayedSeq)
	if err == nil {
		for _, entry := range tailEntries {
			_ = s.applyJournalMutation(ctx, entry, targetNode)
			job.LastReplayedSeq = entry.Seq
		}
	}

	// Audit remote filesystem
	remoteFiles, err := s.storageClient.ListFiles(ctx, targetNode.IP, port)
	if err != nil {
		failMsg := fmt.Sprintf("Final SFTP directory audit failed on %s: %v", targetNode.NodeID, err)
		_ = s.syncJobRepo.FailJob(ctx, targetNode.NodeID, failMsg)
		return fmt.Errorf("%s", failMsg)
	}

	diskFilesMap := make(map[string]bool)
	for _, f := range remoteFiles {
		diskFilesMap[f] = true
	}

	// Verify all active catalog files are present on the new node
	latestActiveFiles, _ := s.fileRepo.FindActive(ctx)
	for _, f := range latestActiveFiles {
		if f.IsDeleted {
			continue
		}
		if !diskFilesMap[f.ID] {
			failMsg := fmt.Sprintf("Audit failure: active file %s (%s) missing from target disk on %s", f.ID, f.OriginalName, targetNode.NodeID)
			_ = s.syncJobRepo.FailJob(ctx, targetNode.NodeID, failMsg)
			_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusError)
			return fmt.Errorf("%s", failMsg)
		}
	}

	// Update MongoDB metadata atomically
	newReplica := models.Replica{
		Location: strings.ToLower(targetNode.Region),
		URL:      targetNode.NodeID,
	}
	_ = s.fileRepo.AddReplicaToActiveFiles(ctx, newReplica)
	_ = s.nodeRepo.UpdateStatus(ctx, targetNode.NodeID, models.NodeStatusOnline)
	_ = s.syncJobRepo.CompleteJob(ctx, targetNode.NodeID)

	log.Printf("[SYNC] Barrier release! Node %s successfully onboarded and activated to ONLINE.", targetNode.NodeID)
	return nil
}

func (s *SyncService) applyJournalMutation(ctx context.Context, entry *models.MutationJournalEntry, targetNode *models.StorageNode) error {
	port := targetNode.Port
	if port <= 0 {
		port = 22
	}

	if entry.Op == models.OpDelete {
		return s.storageClient.Delete(ctx, targetNode.IP, port, entry.FileID)
	}

	if entry.Op == models.OpUpload {
		meta, err := s.fileRepo.FindByID(ctx, entry.FileID)
		if err != nil || meta == nil || meta.IsDeleted {
			return nil // File was subsequently deleted, skip
		}
		return s.transferFileToTarget(ctx, meta, targetNode)
	}

	return nil
}

func (s *SyncService) transferFileToTarget(ctx context.Context, file *models.FileMetadata, targetNode *models.StorageNode) error {
	targetPort := targetNode.Port
	if targetPort <= 0 {
		targetPort = 22
	}

	// 1. Primary: check if local staged copy exists on Central API
	if file.StagedPath != "" && fileExists(file.StagedPath) {
		return s.storageClient.Upload(ctx, targetNode.IP, targetPort, file.ID, file.StagedPath)
	}

	// 2. Secondary: stream from an online healthy replica holding the file
	for _, r := range file.Replicas {
		sourceNode, err := s.nodeRepo.FindByNodeID(ctx, r.URL)
		if err == nil && sourceNode != nil && sourceNode.Status == models.NodeStatusOnline {
			sourcePort := sourceNode.Port
			if sourcePort <= 0 {
				sourcePort = 22
			}
			stream, _, err := s.storageClient.Download(ctx, sourceNode.IP, sourcePort, file.ID)
			if err == nil {
				// Save to temp file on API and upload via rsync
				tempPath := filepath.Join(s.stagingDir, fmt.Sprintf("%s.sync.%d", file.ID, time.Now().UnixNano()))
				dst, err := os.Create(tempPath)
				if err != nil {
					stream.Close()
					continue
				}
				_, _ = io.Copy(dst, stream)
				dst.Close()
				stream.Close()

				uploadErr := s.storageClient.Upload(ctx, targetNode.IP, targetPort, file.ID, tempPath)
				_ = os.Remove(tempPath)
				if uploadErr == nil {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("no available healthy source replica and no local staging copy found for file %s", file.ID)
}
