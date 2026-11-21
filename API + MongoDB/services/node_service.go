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
	IP         string  `json:"ip"`
	Port       int     `json:"port"`
	Region     string  `json:"region"`
	Country    string  `json:"country"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	SSHUser    string  `json:"ssh_user"`
	StorageDir string  `json:"storage_dir"`
}

type DetectedNodeInfo struct {
	IP              string  `json:"ip"`
	Port            int     `json:"port"`
	SSHUser         string  `json:"ssh_user"`
	StorageDir      string  `json:"storage_dir"`
	Country         string  `json:"country"`
	Region          string  `json:"region"`
	City            string  `json:"city"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	HostKeyEnrolled bool    `json:"host_key_enrolled"`
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

	// 0. Auto-enroll host key into known_hosts on first contact (Trust On First Use)
	if s.storageClient != nil {
		if enrollErr := s.storageClient.AutoEnrollHostKey(ctx, ip, port); enrollErr != nil {
			log.Printf("[NODE_MGR] Host key auto-enrollment for %s:%d: %v", ip, port, enrollErr)
		}
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
		return false, errors.New("SSH storage client is uninitialized on Central API server")
	}
	return s.storageClient.Health(ctx, ip, port)
}

// DetectNodeInfo inspects an IP or hostname, resolves geographic coordinates,
// and auto-enrolls the host key (Trust On First Use).
func (s *NodeService) DetectNodeInfo(ctx context.Context, ip string) (*DetectedNodeInfo, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, errors.New("IP address cannot be empty")
	}

	info := &DetectedNodeInfo{
		IP:         ip,
		Port:       22,
		SSHUser:    "ubuntu", // AWS Lightsail standard
		StorageDir: "/var/cdn/data",
		Country:    "Unknown",
		Region:     "Unknown",
	}

	// 1. Identify preset / local testing names
	ipLower := strings.ToLower(ip)
	if strings.Contains(ipLower, "lon") || strings.Contains(ipLower, "uk") {
		info.Region = "Europe"
		info.Country = "United Kingdom"
		info.City = "London"
		info.Latitude = 51.5074
		info.Longitude = -0.1278
	} else if strings.Contains(ipLower, "sg") || strings.Contains(ipLower, "singapore") {
		info.Region = "Asia"
		info.Country = "Singapore"
		info.City = "Singapore"
		info.Latitude = 1.3521
		info.Longitude = 103.8198
	} else if strings.Contains(ipLower, "columbus") || strings.Contains(ipLower, "ohio") {
		info.Region = "US"
		info.Country = "United States"
		info.City = "Columbus"
		info.Latitude = 39.9612
		info.Longitude = -82.9988
	} else if strings.Contains(ipLower, "us") || strings.Contains(ipLower, "virginia") {
		info.Region = "US"
		info.Country = "United States"
		info.City = "New York"
		info.Latitude = 40.7128
		info.Longitude = -74.0060
	} else {
		// 2. Perform public IP geolocation lookup
		cName, rName, lat, lon, err := s.lookupIPGeo(ip)
		if err == nil && (lat != 0 || lon != 0) {
			info.Country = cName
			info.Region = rName
			info.City = rName
			info.Latitude = lat
			info.Longitude = lon
		} else if s.geoService != nil {
			coords := s.geoService.GetCoordinatesForLocation(ip)
			if coords.Latitude != 0 || coords.Longitude != 0 {
				info.Latitude = coords.Latitude
				info.Longitude = coords.Longitude
			}
		}
	}

	// 3. Proactively attempt host key auto-enrollment in the background/inline
	if s.storageClient != nil {
		if err := s.storageClient.AutoEnrollHostKey(ctx, ip, info.Port); err == nil {
			info.HostKeyEnrolled = true
		} else {
			log.Printf("[NODE_MGR] Proactive host key auto-enrollment during detect for %s: %v", ip, err)
		}
	}

	return info, nil
}

func (s *NodeService) lookupIPGeo(ip string) (string, string, float64, float64, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsPrivate() || parsed.IsLoopback() {
		return "", "", 0, 0, errors.New("private or non-IP hostname")
	}

	client := &http.Client{Timeout: 3 * time.Second}

	// Try ipapi.co first
	resp, err := client.Get(fmt.Sprintf("https://ipapi.co/%s/json/", ip))
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var res struct {
			CountryName string  `json:"country_name"`
			City        string  `json:"city"`
			Region      string  `json:"region"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
		}
		if decodeErr := json.NewDecoder(resp.Body).Decode(&res); decodeErr == nil && (res.Latitude != 0 || res.Longitude != 0) {
			region := res.City
			if region == "" {
				region = res.Region
			}
			return res.CountryName, region, res.Latitude, res.Longitude, nil
		}
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Fallback to ip-api.com
	resp2, err := client.Get(fmt.Sprintf("http://ip-api.com/json/%s", ip))
	if err == nil && resp2.StatusCode == http.StatusOK {
		defer resp2.Body.Close()
		var res2 struct {
			Country string  `json:"country"`
			City    string  `json:"city"`
			Region  string  `json:"regionName"`
			Lat     float64 `json:"lat"`
			Lon     float64 `json:"lon"`
		}
		if decodeErr := json.NewDecoder(resp2.Body).Decode(&res2); decodeErr == nil && (res2.Lat != 0 || res2.Lon != 0) {
			region := res2.City
			if region == "" {
				region = res2.Region
			}
			return res2.Country, region, res2.Lat, res2.Lon, nil
		}
	}
	if resp2 != nil {
		resp2.Body.Close()
	}

	return "", "", 0, 0, errors.New("geolocation lookup failed across providers")
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
