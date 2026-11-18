package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/storage_client"
)

var (
	ErrAllReplicasFailed = errors.New("failed to replicate file to any storage node")
	ErrNoAvailableServer = errors.New("no healthy storage node available to serve the file")
)

type FileService struct {
	repo          repository.FileRepository
	nodeRepo      repository.NodeRepository
	storageClient storage_client.StorageClient
	geoService    *geo.GeoService
	storageNodes  []models.Replica
}

func NewFileService(
	repo repository.FileRepository,
	nodeRepo repository.NodeRepository,
	storageClient storage_client.StorageClient,
	geoService *geo.GeoService,
	storageNodes []models.Replica,
) *FileService {
	return &FileService{
		repo:          repo,
		nodeRepo:      nodeRepo,
		storageClient: storageClient,
		geoService:    geoService,
		storageNodes:  storageNodes,
	}
}

// GenerateSafeFileID generates a secure, unique alphanumeric identifier (e.g. "f_1700000000_a1b2c3d4").
func GenerateSafeFileID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("f_%d_%s", time.Now().Unix(), hex.EncodeToString(b))
}

// getActiveReplicationTargets gets active storage nodes dynamically from NodeRepository.
// If none are registered, falls back to default configured storageNodes.
func (s *FileService) getActiveReplicationTargets(ctx context.Context) []models.Replica {
	if s.nodeRepo != nil {
		onlineNodes, err := s.nodeRepo.FindOnline(ctx)
		if err == nil && len(onlineNodes) > 0 {
			var targets []models.Replica
			for _, n := range onlineNodes {
				name := n.Region
				if name == "" || strings.ToLower(name) == "local" {
					name = "singapore"
				}
				targets = append(targets, models.Replica{
					Location: strings.ToLower(name),
					URL:      n.BaseURL(),
				})
			}
			return targets
		}
	}

	var targets []models.Replica
	for _, n := range s.storageNodes {
		loc := n.Location
		if loc == "" || strings.ToLower(loc) == "local" {
			loc = "singapore"
		}
		targets = append(targets, models.Replica{
			Location: strings.ToLower(loc),
			URL:      n.URL,
		})
	}
	return targets
}

func (s *FileService) ResolveClientLocation(r *http.Request) geo.ClientLocation {
	return s.geoService.ResolveClientLocation(r)
}

// Upload handles receiving a file, replicating across storage nodes, and persisting metadata.
func (s *FileService) Upload(
	ctx context.Context,
	filename string,
	contentType string,
	fileReader io.Reader,
	fileSize int64,
) (*models.FileMetadata, *models.ReplicationStatus, error) {
	fileID := GenerateSafeFileID()
	log.Printf("[API] Upload started: file=%s, generatedId=%s, size=%d bytes", filename, fileID, fileSize)

	// Clean filename and infer content type if generic
	filename = filepath.Base(filename)
	if contentType == "" || contentType == "application/octet-stream" {
		ext := filepath.Ext(filename)
		if detected := mime.TypeByExtension(ext); detected != "" {
			contentType = detected
		} else {
			contentType = "application/octet-stream"
		}
	}

	// Read file into memory buffer to allow concurrent replication across nodes
	var fileBuffer bytes.Buffer
	actualSize, err := io.Copy(&fileBuffer, fileReader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to buffer file stream: %w", err)
	}
	if actualSize == 0 {
		return nil, nil, errors.New("cannot upload empty file")
	}

	rawBytes := fileBuffer.Bytes()

	// Discover targets dynamically
	targets := s.getActiveReplicationTargets(ctx)
	if len(targets) == 0 {
		return nil, nil, errors.New("no online storage nodes available for replication")
	}

	var wg sync.WaitGroup
	type repResult struct {
		replica models.Replica
		err     error
	}

	resultChan := make(chan repResult, len(targets))

	// Replicate to each active storage node
	for _, node := range targets {
		wg.Add(1)
		go func(target models.Replica) {
			defer wg.Done()
			uploadReader := bytes.NewReader(rawBytes)
			uploadErr := s.storageClient.Upload(ctx, target.URL, fileID, filename, uploadReader)
			if uploadErr != nil {
				log.Printf("[API] Storage upload FAILED location=%s url=%s error=%v", target.Location, target.URL, uploadErr)
			} else {
				log.Printf("[API] Storage upload SUCCESSFUL location=%s url=%s", target.Location, target.URL)
			}
			resultChan <- repResult{replica: target, err: uploadErr}
		}(node)
	}

	wg.Wait()
	close(resultChan)

	var successfulNodes []models.Replica
	var successfulLocations []string
	var failedLocations []string

	for res := range resultChan {
		if res.err == nil {
			successfulNodes = append(successfulNodes, res.replica)
		} else {
			failedLocations = append(failedLocations, res.replica.Location)
		}
	}

	for _, rep := range successfulNodes {
		successfulLocations = append(successfulLocations, rep.Location)
	}

	if len(successfulNodes) == 0 {
		log.Printf("[API] Upload failed: all %d storage nodes failed", len(targets))
		return nil, nil, ErrAllReplicasFailed
	}

	metadata := &models.FileMetadata{
		ID:           fileID,
		OriginalName: filename,
		ContentType:  contentType,
		Size:         actualSize,
		CreatedAt:    time.Now().UTC(),
		Replicas:     successfulNodes,
	}

	// Persist metadata to MongoDB
	if err := s.repo.Create(ctx, metadata); err != nil {
		log.Printf("[API] MongoDB metadata save FAILED fileId=%s: %v", fileID, err)
		for _, node := range successfulNodes {
			_ = s.storageClient.Delete(ctx, node.URL, fileID)
		}
		return nil, nil, fmt.Errorf("failed to save metadata to MongoDB: %w", err)
	}

	log.Printf("[API] MongoDB metadata saved fileId=%s, replicas=%d", fileID, len(successfulNodes))

	allReplicated := len(failedLocations) == 0
	msg := "Replication complete"
	if !allReplicated {
		msg = "Replication incomplete"
	}

	repStatus := &models.ReplicationStatus{
		Success:            allReplicated,
		Message:            msg,
		SuccessfulReplicas: successfulLocations,
		FailedReplicas:     failedLocations,
	}

	return metadata, repStatus, nil
}

// GetFileStream retrieves the file stream by finding the geographically nearest healthy storage node.
// If the nearest node fails or is down, it gracefully falls back to the next nearest replica.
func (s *FileService) GetFileStream(ctx context.Context, fileID string, r *http.Request) (io.ReadCloser, *models.FileMetadata, string, error) {
	meta, err := s.repo.FindByID(ctx, fileID)
	if err != nil {
		return nil, nil, "", err
	}

	if len(meta.Replicas) == 0 {
		return nil, nil, "", errors.New("file has no registered replicas")
	}

	// 1. Resolve client geographic location
	clientLoc := s.geoService.ResolveClientLocation(r)

	// 2. Rank available storage nodes by proximity to client using actual registered coordinates
	rankedNodes := s.rankReplicas(ctx, clientLoc, meta.Replicas)

	log.Printf("[API] Download request fileId=%s, clientIP=%s, country=%s, rankedNodes=%d",
		fileID, clientLoc.IP, clientLoc.Country, len(rankedNodes))

	// 4. Try nearest replica first; if unavailable, seamlessly fall back to next
	var lastErr error
	for i, node := range rankedNodes {
		isFallback := i > 0
		if isFallback {
			log.Printf("[API] FALLBACK to next nearest replica location=%s (distance=%.1f km) for fileId=%s",
				node.Location, node.DistanceKm, fileID)
		} else {
			log.Printf("[API] Attempting nearest replica location=%s (distance=%.1f km) for fileId=%s",
				node.Location, node.DistanceKm, fileID)
		}

		stream, _, _, err := s.storageClient.Download(ctx, node.URL, fileID)
		if err == nil {
			log.Printf("[API] Successfully serving fileId=%s from storage location=%s", fileID, node.Location)
			return stream, meta, node.Location, nil
		}

		log.Printf("[API] Storage node %s unavailable for fileId=%s: %v", node.Location, fileID, err)
		lastErr = err
	}

	return nil, nil, "", fmt.Errorf("%w: %v", ErrNoAvailableServer, lastErr)
}

// Delete removes the file from all replicas and deletes metadata from MongoDB.
func (s *FileService) Delete(ctx context.Context, fileID string) error {
	meta, err := s.repo.FindByID(ctx, fileID)
	if err != nil {
		return err
	}

	log.Printf("[API] Deleting file fileId=%s across %d replicas", fileID, len(meta.Replicas))

	var wg sync.WaitGroup
	for _, rep := range meta.Replicas {
		wg.Add(1)
		go func(node models.Replica) {
			defer wg.Done()
			delErr := s.storageClient.Delete(ctx, node.URL, fileID)
			if delErr != nil {
				log.Printf("[API] Warning: failed to delete from node %s: %v", node.Location, delErr)
			}
		}(rep)
	}
	wg.Wait()

	if err := s.repo.Delete(ctx, fileID); err != nil {
		return fmt.Errorf("failed to delete metadata from MongoDB: %w", err)
	}

	log.Printf("[API] File deleted successfully fileId=%s", fileID)
	return nil
}

// rankReplicas orders available replicas by proximity to the client using real node coordinates.
func (s *FileService) rankReplicas(ctx context.Context, clientLoc geo.ClientLocation, replicas []models.Replica) []geo.RankedNode {
	var ranked []geo.RankedNode
	var clusterNodes []*models.StorageNode
	if s.nodeRepo != nil {
		clusterNodes, _ = s.nodeRepo.FindAll(ctx)
	}

	nodeMap := make(map[string]*models.StorageNode)
	for _, n := range clusterNodes {
		nodeMap[strings.ToLower(n.Region)] = n
		nodeMap[strings.ToLower(n.Country)] = n
		nodeMap[strings.ToLower(n.NodeID)] = n
		nodeMap[strings.ToLower(n.BaseURL())] = n
	}

	for _, rep := range replicas {
		loc := strings.ToLower(rep.Location)
		if loc == "local" || loc == "" {
			loc = "singapore"
		}

		coords := s.geoService.GetCoordinatesForLocation(loc)
		if n, ok := nodeMap[strings.ToLower(rep.URL)]; ok && (n.Latitude != 0 || n.Longitude != 0) {
			coords = geo.Coordinates{Latitude: n.Latitude, Longitude: n.Longitude}
		} else if n, ok := nodeMap[loc]; ok && (n.Latitude != 0 || n.Longitude != 0) {
			coords = geo.Coordinates{Latitude: n.Latitude, Longitude: n.Longitude}
		}

		dist := geo.Haversine(clientLoc.Coords, coords)
		ranked = append(ranked, geo.RankedNode{
			Location:   loc,
			URL:        rep.URL,
			DistanceKm: math.Round(dist*10) / 10,
		})
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].DistanceKm < ranked[j].DistanceKm
	})
	return ranked
}

// ReconcileReplicas ensures MongoDB file metadata reflects all online storage nodes
// that have the files on disk, and normalizes legacy "local" locations to "singapore".
func (s *FileService) ReconcileReplicas(ctx context.Context) {
	if s.nodeRepo == nil {
		return
	}
	onlineNodes, err := s.nodeRepo.FindOnline(ctx)
	if err != nil || len(onlineNodes) == 0 {
		return
	}

	files, err := s.repo.FindAll(ctx)
	if err != nil {
		return
	}

	for _, file := range files {
		var updated []models.Replica
		nodeMap := make(map[string]bool)

		for _, r := range file.Replicas {
			loc := r.Location
			if loc == "local" || loc == "" {
				loc = "singapore"
			}
			updated = append(updated, models.Replica{
				Location: loc,
				URL:      r.URL,
			})
			nodeMap[r.URL] = true
			nodeMap[strings.ToLower(loc)] = true
		}

		// Check all online nodes in cluster
		for _, n := range onlineNodes {
			locName := strings.ToLower(n.Country)
			if locName == "" || locName == "unknown" {
				locName = strings.ToLower(n.Region)
			}
			if locName == "local" || locName == "" {
				locName = "singapore"
			}

			if !nodeMap[n.BaseURL()] && !nodeMap[locName] {
				// Node is online and synchronized
				updated = append(updated, models.Replica{
					Location: locName,
					URL:      n.BaseURL(),
				})
				nodeMap[n.BaseURL()] = true
				nodeMap[locName] = true
			}
		}

		if len(updated) != len(file.Replicas) || (len(file.Replicas) > 0 && file.Replicas[0].Location == "local") {
			_ = s.repo.UpdateReplicas(ctx, file.ID, updated)
		}
	}
}

// ListFiles lists all file metadata documents and reconciles replicas with online nodes.
func (s *FileService) ListFiles(ctx context.Context) ([]*models.FileMetadata, error) {
	s.ReconcileReplicas(ctx)
	return s.repo.FindAll(ctx)
}

// GetMetadata returns metadata for a single file.
func (s *FileService) GetMetadata(ctx context.Context, fileID string) (*models.FileMetadata, error) {
	s.ReconcileReplicas(ctx)
	return s.repo.FindByID(ctx, fileID)
}

// GetLocationDebug computes dynamic distance metrics for all currently registered storage nodes in the cluster.
func (s *FileService) GetLocationDebug(ctx context.Context, fileID string, r *http.Request) (*models.LocationDebugResponse, error) {
	s.ReconcileReplicas(ctx)

	meta, err := s.repo.FindByID(ctx, fileID)
	if err != nil {
		return nil, err
	}

	clientLoc := s.geoService.ResolveClientLocation(r)

	// Reconcile and normalize file replicas
	var availableReplicas []string
	replicaMap := make(map[string]bool)
	for _, rep := range meta.Replicas {
		loc := strings.ToLower(rep.Location)
		if loc == "local" || loc == "" {
			loc = "singapore"
		}
		if !replicaMap[loc] {
			replicaMap[loc] = true
			availableReplicas = append(availableReplicas, loc)
		}
	}

	// Dynamic calculation of distances: ONLY for actually registered cluster nodes in MongoDB!
	distances := make(map[string]float64)
	var clusterNodes []*models.StorageNode
	if s.nodeRepo != nil {
		clusterNodes, _ = s.nodeRepo.FindAll(ctx)
	}

	if len(clusterNodes) > 0 {
		for _, n := range clusterNodes {
			loc := strings.ToLower(n.Country)
			if loc == "" || loc == "unknown" {
				loc = strings.ToLower(n.Region)
			}
			if loc == "local" || loc == "" {
				loc = "singapore"
			}

			coords := geo.Coordinates{Latitude: n.Latitude, Longitude: n.Longitude}
			if coords.Latitude == 0 && coords.Longitude == 0 {
				coords = s.geoService.GetCoordinatesForLocation(loc)
			}
			dist := geo.Haversine(clientLoc.Coords, coords)
			distances[loc] = math.Round(dist*10) / 10

			// Check if file is available on this node by URL or location
			for _, r := range meta.Replicas {
				if r.URL == n.BaseURL() || strings.EqualFold(r.Location, loc) || strings.EqualFold(r.Location, n.Region) {
					if !replicaMap[loc] {
						replicaMap[loc] = true
						availableReplicas = append(availableReplicas, loc)
					}
				}
			}
		}
	} else {
		// Fallback only if no dynamic nodes exist
		for _, node := range s.storageNodes {
			loc := strings.ToLower(node.Location)
			if loc == "local" || loc == "" {
				loc = "singapore"
			}
			coords := s.geoService.GetCoordinatesForLocation(loc)
			dist := geo.Haversine(clientLoc.Coords, coords)
			distances[loc] = math.Round(dist*10) / 10
		}
	}

	// Calculate Nearest Storage:
	// Find the available replica with minimum distance to the client
	nearest := "unknown"
	minDist := math.MaxFloat64
	for _, repLoc := range availableReplicas {
		if d, ok := distances[repLoc]; ok {
			if d < minDist {
				minDist = d
				nearest = repLoc
			}
		}
	}
	// Fallback to nearest registered node if not resolved
	if nearest == "unknown" && len(distances) > 0 {
		for loc, d := range distances {
			if d < minDist {
				minDist = d
				nearest = loc
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

	targets := s.getActiveReplicationTargets(ctx)

	for _, node := range targets {
		wg.Add(1)
		go func(n models.Replica) {
			defer wg.Done()
			online, loc, err := s.storageClient.Health(ctx, n.URL)
			statusStr := "ONLINE"
			errStr := ""
			if !online || err != nil {
				statusStr = "OFFLINE"
				if err != nil {
					errStr = err.Error()
				}
			}
			if loc == "" {
				loc = n.Location
			}

			mu.Lock()
			storageHealth = append(storageHealth, models.NodeHealth{
				Name:     fmt.Sprintf("Storage (%s)", n.Location),
				Location: loc,
				URL:      n.URL,
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
