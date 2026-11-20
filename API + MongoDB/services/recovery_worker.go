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

// ReplicationRecoveryWorker runs in the background to retry incomplete or partial replications.
type ReplicationRecoveryWorker struct {
	fileRepo      repository.FileRepository
	nodeRepo      repository.NodeRepository
	storageClient ssh_client.StorageClient
	barrier       *barrier.MutationBarrier
	stagingDir    string
	interval      time.Duration
	stopChan      chan struct{}
}

func NewReplicationRecoveryWorker(
	fileRepo repository.FileRepository,
	nodeRepo repository.NodeRepository,
	storageClient ssh_client.StorageClient,
	barrier *barrier.MutationBarrier,
	stagingDir string,
	interval time.Duration,
) *ReplicationRecoveryWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &ReplicationRecoveryWorker{
		fileRepo:      fileRepo,
		nodeRepo:      nodeRepo,
		storageClient: storageClient,
		barrier:       barrier,
		stagingDir:    stagingDir,
		interval:      interval,
		stopChan:      make(chan struct{}),
	}
}

func (w *ReplicationRecoveryWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.RunRecoveryCycle(ctx)
			case <-w.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (w *ReplicationRecoveryWorker) Stop() {
	close(w.stopChan)
}

func (w *ReplicationRecoveryWorker) RunRecoveryCycle(ctx context.Context) {
	// Acquire Mutation Barrier Read Lock
	w.barrier.RLock()
	defer w.barrier.RUnlock()

	pendingFiles, err := w.fileRepo.FindPendingReplication(ctx)
	if err != nil || len(pendingFiles) == 0 {
		return
	}

	onlineNodes, err := w.nodeRepo.FindOnline(ctx)
	if err != nil || len(onlineNodes) == 0 {
		return
	}

	totalNodesCount := len(onlineNodes)

	for _, file := range pendingFiles {
		w.recoverFile(ctx, file, onlineNodes, totalNodesCount)
	}
}

func (w *ReplicationRecoveryWorker) recoverFile(ctx context.Context, file *models.FileMetadata, onlineNodes []*models.StorageNode, totalNodesCount int) {
	// Build map of existing replicas
	existing := make(map[string]bool)
	for _, r := range file.Replicas {
		existing[r.URL] = true
	}

	// 1. Identify missing nodes
	var missingNodes []*models.StorageNode
	for _, n := range onlineNodes {
		if !existing[n.NodeID] {
			missingNodes = append(missingNodes, n)
		}
	}

	if len(missingNodes) == 0 {
		if len(file.Replicas) >= 3 && len(file.Replicas) >= totalNodesCount {
			_ = w.fileRepo.UpdateReplicationStatus(ctx, file.ID, models.ReplicationStatusComplete, true, file.Replicas, "")
			if file.StagedPath != "" {
				_ = os.Remove(file.StagedPath)
			}
		}
		return
	}

	// 2. Locate source file
	sourcePath := file.StagedPath
	needCleanupSource := false

	if sourcePath == "" || !fileExists(sourcePath) {
		// Attempt to download from an existing online replica to a temp staging file
		for _, r := range file.Replicas {
			node, err := w.nodeRepo.FindByNodeID(ctx, r.URL)
			if err == nil && node != nil {
				port := node.Port
				if port <= 0 {
					port = 22
				}
				stream, _, err := w.storageClient.Download(ctx, node.IP, port, file.ID)
				if err == nil {
					tempPath := filepath.Join(w.stagingDir, fmt.Sprintf("%s.tmp.%d", file.ID, time.Now().UnixNano()))
					dst, err := os.Create(tempPath)
					if err == nil {
						_, _ = io.Copy(dst, stream)
						dst.Close()
						stream.Close()
						sourcePath = tempPath
						needCleanupSource = true
						break
					}
					stream.Close()
				}
			}
		}
	}

	if sourcePath == "" || !fileExists(sourcePath) {
		log.Printf("[RECOVERY] File %s has no local staged copy and no reachable online replicas for recovery.", file.ID)
		return
	}

	if needCleanupSource {
		defer os.Remove(sourcePath)
	}

	// 3. Replicate to missing nodes
	newReplicas := file.Replicas
	for _, node := range missingNodes {
		port := node.Port
		if port <= 0 {
			port = 22
		}
		err := w.storageClient.Upload(ctx, node.IP, port, file.ID, sourcePath)
		if err == nil {
			loc := node.Region
			if loc == "" {
				loc = node.Country
			}
			newReplicas = append(newReplicas, models.Replica{
				Location: strings.ToLower(loc),
				URL:      node.NodeID,
			})
			log.Printf("[RECOVERY] Replicated missing file %s to node %s (%s:%d)", file.ID, node.NodeID, node.IP, port)
		}
	}

	// 4. Update status in MongoDB
	newCount := len(newReplicas)
	quorumMet := newCount >= 2
	status := models.ReplicationStatusPartial
	persistedStagedPath := file.StagedPath

	if newCount >= 3 && newCount >= totalNodesCount {
		status = models.ReplicationStatusComplete
		persistedStagedPath = ""
		if file.StagedPath != "" {
			_ = os.Remove(file.StagedPath)
		}
		log.Printf("[RECOVERY] 3/3 replication satisfied for file %s. Status changed to COMPLETE.", file.ID)
	}

	_ = w.fileRepo.UpdateReplicationStatus(ctx, file.ID, status, quorumMet, newReplicas, persistedStagedPath)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
