package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/ssh_client"
)

var (
	ErrWorkerUnreachable = errors.New("storage node unreachable via SSH or invalid storage directory")
	ErrDuplicateNode     = errors.New("storage node with this IP and port is already registered")
)

type RegisterNodeParams struct {
	IP        string  `json:"ip"`
	Port      int     `json:"port"`
	Region    string  `json:"region"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type NodeService struct {
	repo          repository.NodeRepository
	syncService   *SyncService
	fileRepo      repository.FileRepository
	geoService    *geo.GeoService
	storageClient ssh_client.StorageClient
}

func NewNodeService(
	repo repository.NodeRepository,
	syncService *SyncService,
	storageClient ssh_client.StorageClient,
) *NodeService {
	return &NodeService{
		repo:          repo,
		syncService:   syncService,
		storageClient: storageClient,
	}
}

func (s *NodeService) SetFileRepo(fileRepo repository.FileRepository) {
	s.fileRepo = fileRepo
}

func (s *NodeService) SetGeoService(geoService *geo.GeoService) {
	s.geoService = geoService
}

// OnboardNode executes the verification, persistent registration, and two-phase synchronization:
// 1. Validate request
// 2. SSH health & host-key check
// 3. Create node in PENDING state
// 4. Delegate to SyncService (Two-phase journal replay + final barrier)
func (s *NodeService) OnboardNode(ctx context.Context, params RegisterNodeParams) (*models.StorageNode, error) {
	ip := strings.TrimSpace(params.IP)
	if ip == "" {
		return nil, errors.New("IP address or hostname cannot be empty")
	}

	port := params.Port
	if port <= 0 {
		port = 22
	}

	region := strings.TrimSpace(params.Region)
	if region == "" {
		region = "Unknown"
	}

	country := strings.TrimSpace(params.Country)
	if country == "" {
		country = region
	}

	// 1. Health check worker via strict SSH
	healthy, err := s.probeWorkerHealth(ctx, ip, port)
	if err != nil || !healthy {
		log.Printf("[NODE_MGR] SSH storage health check failed for %s:%d: %v", ip, port, err)
		return nil, fmt.Errorf("%w: %v", ErrWorkerUnreachable, err)
	}

	// 2. Check duplicate
	existing, _ := s.repo.FindByIPAndPort(ctx, ip, port)
	if existing != nil {
		return nil, ErrDuplicateNode
	}

	nodeID := fmt.Sprintf("node-%s-%d", strings.ToLower(region), port)
	if n, _ := s.repo.FindByNodeID(ctx, nodeID); n != nil {
		nodeID = fmt.Sprintf("%s-%s", nodeID, uuid.New().String()[:6])
	}

	lat := params.Latitude
	lon := params.Longitude
	if lat == 0 && lon == 0 {
		if cName, rName, ipLat, ipLon, err := s.lookupIPGeo(ip); err == nil && (ipLat != 0 || ipLon != 0) {
			lat = ipLat
			lon = ipLon
			if country == "" || country == "Unknown" || country == "Auto" {
				country = cName
			}
			if region == "" || region == "Unknown" || region == "Auto" {
				region = rName
			}
		} else if s.geoService != nil {
			coords := s.geoService.GetCoordinatesForLocation(country)
			if coords.Latitude == 0 && coords.Longitude == 0 {
				coords = s.geoService.GetCoordinatesForLocation(region)
			}
			lat = coords.Latitude
			lon = coords.Longitude
		}
	}
	if country == "" || country == "Unknown" || country == "Auto" {
		country = region
	}
	if region == "" || region == "Unknown" || region == "Auto" {
		region = country
	}
	if region == "" {
		region = "Asia"
		country = "Asia"
	}

	now := time.Now().UTC()
	node := &models.StorageNode{
		ID:            uuid.New().String(),
		NodeID:        nodeID,
		IP:            ip,
		Port:          port,
		Region:        region,
		Country:       country,
		Latitude:      lat,
		Longitude:     lon,
		Status:        models.NodeStatusPending,
		LastHeartbeat: now,
		CreatedAt:     now,
	}

	// 3. Save as PENDING
	if err := s.repo.Create(ctx, node); err != nil {
		return nil, fmt.Errorf("failed to save node to repository: %w", err)
	}

	log.Printf("[NODE_MGR] Node %s (%s:%d) saved in PENDING state. Starting synchronization...", node.NodeID, node.IP, node.Port)

	// 4. Delegate to SyncService for Two-Phase Sync & Barrier Activation
	if s.syncService != nil {
		if err := s.syncService.OnboardAndSynchronize(ctx, node); err != nil {
			log.Printf("[NODE_MGR] Synchronization failed for node %s: %v", node.NodeID, err)
			return node, fmt.Errorf("node registered but data synchronization failed: %w", err)
		}
	} else {
		_ = s.repo.UpdateStatus(ctx, node.ID, models.NodeStatusOnline)
	}

	node.Status = models.NodeStatusOnline
	log.Printf("[NODE_MGR] Node %s successfully onboarded and activated (ONLINE)", node.NodeID)
	return node, nil
}

func (s *NodeService) probeWorkerHealth(ctx context.Context, ip string, port int) (bool, error) {
	if s.storageClient == nil {
		return true, nil
	}
	return s.storageClient.Health(ctx, ip, port)
}

func (s *NodeService) lookupIPGeo(ip string) (string, string, float64, float64, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsPrivate() || parsed.IsLoopback() {
		return "", "", 0, 0, errors.New("private or non-IP hostname")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://ipapi.co/%s/json/", ip))
	if err != nil {
		return "", "", 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", 0, 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	var res struct {
		CountryName string  `json:"country_name"`
		City        string  `json:"city"`
		Region      string  `json:"region"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", "", 0, 0, err
	}
	region := res.City
	if region == "" {
		region = res.Region
	}
	return res.CountryName, region, res.Latitude, res.Longitude, nil
}

func (s *NodeService) GetAllNodes(ctx context.Context) ([]*models.StorageNode, error) {
	return s.repo.FindAll(ctx)
}

func (s *NodeService) GetOnlineNodes(ctx context.Context) ([]*models.StorageNode, error) {
	return s.repo.FindOnline(ctx)
}

func (s *NodeService) GetNode(ctx context.Context, id string) (*models.StorageNode, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *NodeService) RemoveNode(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *NodeService) CheckNodeHealth(ctx context.Context, id string) (*models.StorageNode, error) {
	node, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	healthy, probeErr := s.probeWorkerHealth(ctx, node.IP, node.Port)
	now := time.Now().UTC()
	if probeErr != nil || !healthy {
		_ = s.repo.UpdateHeartbeat(ctx, node.ID, models.NodeStatusOffline, now)
		node.Status = models.NodeStatusOffline
		return node, probeErr
	}

	_ = s.repo.UpdateHeartbeat(ctx, node.ID, models.NodeStatusOnline, now)
	node.Status = models.NodeStatusOnline
	node.LastHeartbeat = now
	return node, nil
}

// StartHeartbeatMonitor runs periodic health checks on all registered nodes in background.
func (s *NodeService) StartHeartbeatMonitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			nodes, err := s.repo.FindAll(ctx)
			if err == nil {
				for _, n := range nodes {
					_, _ = s.CheckNodeHealth(ctx, n.ID)
				}
			}
			cancel()
		}
	}()
}
