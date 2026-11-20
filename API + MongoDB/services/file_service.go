package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services/barrier"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/ssh_client"
)

var (
	ErrAllReplicasFailed = errors.New("failed to replicate file to any storage node")
	ErrNoAvailableServer = errors.New("no healthy storage node available to serve the file")
	ErrQuorumNotMet      = errors.New("quorum not met (fewer than 2 replicas available)")
)

type FileService struct {
	repo          repository.FileRepository
	nodeRepo      repository.NodeRepository
	storageClient ssh_client.StorageClient
	geoService    *geo.GeoService
	barrier       *barrier.MutationBarrier
	storageNodes  []models.Replica
}

func NewFileService(
	repo repository.FileRepository,
	nodeRepo repository.NodeRepository,
	storageClient ssh_client.StorageClient,
	geoService *geo.GeoService,
	barrier *barrier.MutationBarrier,
	storageNodes []models.Replica,
) *FileService {
	return &FileService{
		repo:          repo,
		nodeRepo:      nodeRepo,
		storageClient: storageClient,
		geoService:    geoService,
		barrier:       barrier,
		storageNodes:  storageNodes,
	}
}

// GenerateSafeFileID generates a secure, unique alphanumeric identifier (e.g. "f_1700000000_a1b2c3d4").
func GenerateSafeFileID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("f_%d_%s", time.Now().Unix(), hex.EncodeToString(b))
}

type targetNode struct {
	nodeID   string
	ip       string
	port     int
	location string
}

// getActiveStorageNodes resolves the active online nodes from NodeRepository.
func (s *FileService) getActiveStorageNodes(ctx context.Context) []targetNode {
	if s.nodeRepo != nil {
		onlineNodes, err := s.nodeRepo.FindOnline(ctx)
		if err == nil && len(onlineNodes) > 0 {
			var targets []targetNode
			for _, n := range onlineNodes {
				port := n.Port
				if port <= 0 {
					port = 22
				}
				loc := n.Region
				if loc == "" {
					loc = n.Country
				}
				if loc == "" {
					loc = "storage"
				}
				targets = append(targets, targetNode{
					nodeID:   n.NodeID,
					ip:       n.IP,
					port:     port,
					location: strings.ToLower(loc),
				})
			}
			return targets
		}
	}

	// Fallback to configured storage nodes
	var targets []targetNode
	for i, n := range s.storageNodes {
		host := n.URL
		port := 22
		cleanHost := strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://")
		if strings.Contains(cleanHost, ":") {
			parts := strings.Split(cleanHost, ":")
			cleanHost = parts[0]
			if p, err := strconv.Atoi(parts[1]); err == nil {
				port = p
			}
		}
		nodeID := fmt.Sprintf("node-%s-%d", n.Location, i+1)
		targets = append(targets, targetNode{
			nodeID:   nodeID,
			ip:       cleanHost,
			port:     port,
			location: strings.ToLower(n.Location),
		})
	}
	return targets
}

func (s *FileService) ResolveClientLocation(r *http.Request) geo.ClientLocation {
	return s.geoService.ResolveClientLocation(r)
}

// Upload receives a stream, saves it to an ephemeral temp file, computes SHA-256 and size,
// replicates across active storage nodes via rsync over SSH, enforces 3/3 replication for COMPLETE,
// updates MongoDB, and cleans up the ephemeral temp file immediately.
func (s *FileService) Upload(
	ctx context.Context,
	filename string,
	contentType string,
	fileReader io.Reader,
	fileSize int64,
) (*models.FileMetadata, *models.ReplicationStatus, error) {
	// 1. Acquire Mutation Barrier Read Lock
	s.barrier.RLock()
	defer s.barrier.RUnlock()

	fileID := GenerateSafeFileID()
	log.Printf("[API] Upload initiated: file=%s, fileId=%s", filename, fileID)

	filename = filepath.Base(filename)
	if contentType == "" || contentType == "application/octet-stream" {
		ext := filepath.Ext(filename)
		if detected := mime.TypeByExtension(ext); detected != "" {
			contentType = detected
		} else {
			contentType = "application/octet-stream"
		}
	}

	// 2. Stream to ephemeral local temporary file on API disk
	tempFile, err := os.CreateTemp("", "cdn-upload-*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temporary upload file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	hasher := sha256.New()
	mw := io.MultiWriter(tempFile, hasher)

	actualSize, copyErr := io.Copy(mw, fileReader)
	if copyErr != nil {
		return nil, nil, fmt.Errorf("failed to write upload file: %w", copyErr)
	}
	_ = tempFile.Close() // Close handle so rsync can read cleanly

	if actualSize == 0 {
		return nil, nil, errors.New("cannot upload empty file")
	}

	sha256Hex := hex.EncodeToString(hasher.Sum(nil))

	// 3. Save initial metadata record in MongoDB
	fileMeta := &models.FileMetadata{
		ID:                fileID,
		OriginalName:      filename,
		ContentType:       contentType,
		Size:              actualSize,
		SHA256:            sha256Hex,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
		ReplicationStatus: models.ReplicationStatusPending,
		QuorumMet:         false,
		Replicas:          []models.Replica{},
		IsDeleted:         false,
	}

	if err := s.repo.Create(ctx, fileMeta); err != nil {
		return nil, nil, fmt.Errorf("failed to create file metadata: %w", err)
	}

	// 4. Discover active replication targets
	targets := s.getActiveStorageNodes(ctx)
	if len(targets) == 0 {
		log.Printf("[API] Warning: no active storage nodes registered for %s", fileID)
		_ = s.repo.UpdateReplicationStatus(ctx, fileID, models.ReplicationStatusFailed, false, nil)
		repStatus := &models.ReplicationStatus{
			Success:            false,
			Message:            "Zero storage nodes currently online.",
			SuccessfulReplicas: []string{},
			FailedReplicas:     []string{"all"},
		}
		return fileMeta, repStatus, ErrAllReplicasFailed
	}

	// 5. Parallel rsync replication over SSH to all active nodes
	type repResult struct {
		target targetNode
		err    error
	}

	resultChan := make(chan repResult, len(targets))
	var wg sync.WaitGroup

	for _, t := range targets {
		wg.Add(1)
		go func(node targetNode) {
			defer wg.Done()
			repErr := s.storageClient.Upload(ctx, node.ip, node.port, fileID, tempPath)
			if repErr != nil {
				log.Printf("[API] Replication to %s (%s:%d) FAILED: %v", node.nodeID, node.ip, node.port, repErr)
			} else {
				log.Printf("[API] Replication to %s (%s:%d) SUCCESSFUL", node.nodeID, node.ip, node.port)
			}
			resultChan <- repResult{target: node, err: repErr}
		}(t)
	}

	wg.Wait()
	close(resultChan)

	var successfulNodes []models.Replica
	var successfulNames []string
	var failedNames []string

	for r := range resultChan {
		if r.err == nil {
			successfulNodes = append(successfulNodes, models.Replica{
				Location: r.target.location,
				URL:      r.target.nodeID,
			})
			successfulNames = append(successfulNames, r.target.nodeID)
		} else {
			failedNames = append(failedNames, r.target.nodeID)
		}
	}

	// 6. Enforce strict 3/3 replication rules:
	// 3/3 successful = COMPLETE
	// 1/3 or 2/3 successful = PARTIAL
	// 0/3 = FAILED
	totalRequired := len(targets)
	successCount := len(successfulNodes)

	var finalStatus string
	quorumMet := successCount >= 2

	if successCount == totalRequired && successCount >= 3 {
		finalStatus = models.ReplicationStatusComplete
	} else if successCount >= 1 {
		finalStatus = models.ReplicationStatusPartial
	} else {
		finalStatus = models.ReplicationStatusFailed
	}

	// 7. Update MongoDB with verified replicas only
	fileMeta.ReplicationStatus = finalStatus
	fileMeta.QuorumMet = quorumMet
	fileMeta.Replicas = successfulNodes

	_ = s.repo.UpdateReplicationStatus(ctx, fileID, finalStatus, quorumMet, successfulNodes)

	if successCount == 0 {
		return fileMeta, nil, ErrAllReplicasFailed
	}

	repResultObj := &models.ReplicationStatus{
		Success:            finalStatus == models.ReplicationStatusComplete,
		Message:            fmt.Sprintf("Replication: %s (%d/%d verified)", finalStatus, successCount, totalRequired),
		SuccessfulReplicas: successfulNames,
		FailedReplicas:     failedNames,
	}

	return fileMeta, repResultObj, nil
}

// GetFileStream retrieves the file stream by finding the geographically nearest healthy storage node.
func (s *FileService) GetFileStream(ctx context.Context, fileID string, r *http.Request) (io.ReadCloser, *models.FileMetadata, string, error) {
	meta, err := s.repo.FindByID(ctx, fileID)
	if err != nil {
		return nil, nil, "", err
	}

	// 1. Resolve client geographic location
	clientLoc := s.geoService.ResolveClientLocation(r)

	// 2. Rank available replicas by proximity
	rankedNodes := s.rankReplicas(ctx, clientLoc, meta.Replicas)

	// 3. Attempt download from nearest replica over SFTP
	for _, node := range rankedNodes {
		host, port := s.resolveNodeHostPort(ctx, node.URL)
		stream, size, err := s.storageClient.Download(ctx, host, port, fileID)
		if err == nil {
			_ = size
			log.Printf("[API] Streaming file %s from storage node %s (%s:%d)", fileID, node.Location, host, port)
			return stream, meta, node.Location, nil
		}
		log.Printf("[API] Failed to stream from replica %s (%s:%d): %v, falling back...", node.Location, host, port, err)
	}

	return nil, nil, "", ErrNoAvailableServer
}

// Delete marks a file as deleted in MongoDB and executes SFTP Remove across all replicas holding the file.
func (s *FileService) Delete(ctx context.Context, fileID string) error {
	// 1. Acquire Mutation Barrier Read Lock
	s.barrier.RLock()
	defer s.barrier.RUnlock()

	meta, err := s.repo.FindByID(ctx, fileID)
	if err != nil {
		return err
	}

	// 2. Soft delete / tombstone metadata in MongoDB
	if err := s.repo.Delete(ctx, fileID); err != nil {
		return err
	}

	// 3. Execute SFTP Remove across all registered replicas holding the file
	var wg sync.WaitGroup
	for _, rep := range meta.Replicas {
		wg.Add(1)
		go func(r models.Replica) {
			defer wg.Done()
			host, port := s.resolveNodeHostPort(ctx, r.URL)
			if err := s.storageClient.Delete(ctx, host, port, fileID); err != nil {
				log.Printf("[API] SFTP Remove on replica %s failed: %v", r.Location, err)
			}
		}(rep)
	}
	wg.Wait()

	return nil
}

func (s *FileService) resolveNodeHostPort(ctx context.Context, nodeIdentifier string) (string, int) {
	if s.nodeRepo != nil {
		node, err := s.nodeRepo.FindByNodeID(ctx, nodeIdentifier)
		if err == nil && node != nil {
			port := node.Port
			if port <= 0 {
				port = 22
			}
			return node.IP, port
		}
	}

	// Check configured fallback nodes
	for i, n := range s.storageNodes {
		synthID := fmt.Sprintf("node-%s-%d", n.Location, i+1)
		cleanHost := strings.TrimPrefix(strings.TrimPrefix(n.URL, "http://"), "https://")
		port := 22
		if strings.Contains(cleanHost, ":") {
			parts := strings.Split(cleanHost, ":")
			cleanHost = parts[0]
			if p, err := strconv.Atoi(parts[1]); err == nil && p > 0 {
				port = p
			}
		}
		if synthID == nodeIdentifier || n.URL == nodeIdentifier || strings.EqualFold(n.Location, nodeIdentifier) || strings.Contains(nodeIdentifier, strings.ToLower(n.Location)) {
			return cleanHost, port
		}
	}

	// Direct IP:port or hostname:port string
	clean := strings.TrimPrefix(strings.TrimPrefix(nodeIdentifier, "http://"), "https://")
	if strings.Contains(clean, ":") {
		parts := strings.Split(clean, ":")
		p, _ := strconv.Atoi(parts[1])
		if p <= 0 {
			p = 22
		}
		return parts[0], p
	}
	return clean, 22
}

func (s *FileService) rankReplicas(ctx context.Context, clientLoc geo.ClientLocation, replicas []models.Replica) []models.Replica {
	type scoredReplica struct {
		replica  models.Replica
		distance float64
	}

	var scored []scoredReplica
	for _, rep := range replicas {
		var lat, lon float64
		found := false

		if s.nodeRepo != nil {
			node, err := s.nodeRepo.FindByNodeID(ctx, rep.URL)
			if err == nil && node != nil && (node.Latitude != 0 || node.Longitude != 0) {
				lat = node.Latitude
				lon = node.Longitude
				found = true
			}
		}

		if !found {
			switch strings.ToLower(rep.Location) {
			case "singapore", "asia":
				lat, lon = 1.3521, 103.8198
			case "newyork", "us", "north america":
				lat, lon = 40.7128, -74.0060
			case "london", "europe":
				lat, lon = 51.5074, -0.1278
			default:
				lat, lon = 0, 0
			}
		}

		dist := geo.Haversine(clientLoc.Coords, geo.Coordinates{Latitude: lat, Longitude: lon})
		scored = append(scored, scoredReplica{replica: rep, distance: dist})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].distance < scored[j].distance
	})

	var result []models.Replica
	for _, sc := range scored {
		result = append(result, sc.replica)
	}
	return result
}

// ListFiles returns all active file metadata records.
func (s *FileService) ListFiles(ctx context.Context) ([]*models.FileMetadata, error) {
	return s.repo.FindAll(ctx)
}

// GetLocationDebug provides diagnostic information regarding nearest server calculation.
func (s *FileService) GetLocationDebug(ctx context.Context, fileID string, r *http.Request) (*models.LocationDebugResponse, error) {
	meta, err := s.repo.FindByID(ctx, fileID)
	if err != nil {
		return nil, err
	}

	clientLoc := s.geoService.ResolveClientLocation(r)

	var availableReplicas []string
	replicaMap := make(map[string]bool)
	for _, rep := range meta.Replicas {
		loc := strings.ToLower(rep.Location)
		if !replicaMap[loc] {
			replicaMap[loc] = true
			availableReplicas = append(availableReplicas, loc)
		}
	}

	distances := make(map[string]float64)
	targets := s.getActiveStorageNodes(ctx)
	for _, t := range targets {
		coords := s.geoService.GetCoordinatesForLocation(t.location)
		dist := geo.Haversine(clientLoc.Coords, coords)
		distances[t.location] = float64(int(dist*10)) / 10
	}

	nearest := "unknown"
	minDist := 1e12
	for _, repLoc := range availableReplicas {
		if d, ok := distances[repLoc]; ok {
			if d < minDist {
				minDist = d
				nearest = repLoc
			}
		}
	}

	return &models.LocationDebugResponse{
		FileID:            fileID,
		ClientIP:          clientLoc.IP,
		ClientCountry:     clientLoc.Country,
		NearestStorage:    nearest,
		Distances:         distances,
		AvailableReplicas: availableReplicas,
	}, nil
}

// GetSystemStatus gathers health status across API, MongoDB, and Storage nodes.
func (s *FileService) GetSystemStatus(ctx context.Context) *models.SystemStatusResponse {
	mongoStatus := "ONLINE"
	if err := s.repo.Ping(ctx); err != nil {
		mongoStatus = "OFFLINE"
	}

	var storageHealth []models.NodeHealth
	var wg sync.WaitGroup
	var mu sync.Mutex

	targets := s.getActiveStorageNodes(ctx)
	for _, node := range targets {
		wg.Add(1)
		go func(t targetNode) {
			defer wg.Done()
			online, err := s.storageClient.Health(ctx, t.ip, t.port)
			statusStr := "ONLINE"
			errStr := ""
			if !online || err != nil {
				statusStr = "OFFLINE"
				if err != nil {
					errStr = err.Error()
				}
			}

			mu.Lock()
			storageHealth = append(storageHealth, models.NodeHealth{
				Name:     fmt.Sprintf("Storage (%s)", t.location),
				Location: t.location,
				URL:      fmt.Sprintf("%s:%d", t.ip, t.port),
				Status:   statusStr,
				Error:    errStr,
			})
			mu.Unlock()
		}(node)
	}
	wg.Wait()

	return &models.SystemStatusResponse{
		API:     "ONLINE",
		MongoDB: mongoStatus,
		Storage: storageHealth,
	}
}
