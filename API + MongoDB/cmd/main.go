package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"mini-cdn/api/handlers"
	"mini-cdn/api/handlers/middleware"
	"mini-cdn/api/internal/config"
	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services"
	"mini-cdn/api/services/barrier"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/ssh_client"
)

func main() {
	cfg := config.Load()

	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	log.Printf("[API] Starting Central API Coordinator on port %s (env: %s)...", cfg.Port, cfg.Env)
	log.Printf("[API] Connecting to MongoDB at %s (db: %s)...", cfg.MongoURI, cfg.MongoDB)

	// MongoDB connection context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo, err := repository.NewMongoFileRepository(ctx, cfg.MongoURI, cfg.MongoDB)
	var nodeRepo repository.NodeRepository

	if err != nil {
		log.Printf("[API] WARNING: Could not connect to MongoDB initially: %v", err)
		log.Printf("[API] Using in-memory repositories fallback until MongoDB is online.")
		nodeRepo = repository.NewMockNodeRepository()
	} else {
		log.Println("[API] Successfully connected to MongoDB.")
		nodeRepo = repository.NewMongoNodeRepository(repo.Client(), cfg.MongoDB)
	}

	// Geolocation service
	geoService := geo.NewGeoService()

	// Mutation Barrier Mutex
	barrierMutex := barrier.NewMutationBarrier()

	// Strict SSH & rsync storage client
	storageClient, err := ssh_client.NewSSHStorageClient(cfg.SSHKeyPath, cfg.KnownHostsPath, cfg.SSHUser, cfg.StorageDataDir)
	if err != nil {
		log.Printf("[API] WARNING: Could not initialize SSH client: %v. Running in mock/fallback mode.", err)
	} else {
		log.Printf("[API] Strict SSH storage client initialized (Key: %s, KnownHosts: %s).", cfg.SSHKeyPath, cfg.KnownHostsPath)
	}

	// Sync service & Node service
	syncService := services.NewSyncService(repo, nodeRepo, storageClient, barrierMutex)
	nodeService := services.NewNodeService(nodeRepo, syncService, storageClient)
	nodeService.SetFileRepo(repo)
	nodeService.SetGeoService(geoService)

	// Seed default nodes if repository is empty
	seedInitialNodes(context.Background(), nodeRepo, geoService, cfg.StorageNodes)

	// Start background node health monitor
	nodeService.StartHeartbeatMonitor(20 * time.Second)

	// File Service
	fileService := services.NewFileService(repo, nodeRepo, storageClient, geoService, barrierMutex, cfg.StorageNodes)
	fileHandler := handlers.NewFileHandler(fileService, cfg.MaxUploadSizeMB)
	nodeHandler := handlers.NewNodeHandler(nodeService)

	// Initialize Gin engine
	r := gin.Default()
	r.Use(handlers.CORSMiddleware())

	// Health and Status
	r.GET("/health", fileHandler.Health)
	r.GET("/api/status", fileHandler.Status)
	r.GET("/api/client-location", fileHandler.ClientLocation)

	// File API endpoints
	apiFiles := r.Group("/api/files")
	{
		apiFiles.GET("", fileHandler.List)
		apiFiles.POST("", fileHandler.Upload)
		apiFiles.GET("/:id", fileHandler.Get)
		apiFiles.GET("/:id/view", fileHandler.Get)
		apiFiles.DELETE("/:id", fileHandler.Delete)
		apiFiles.GET("/:id/location", fileHandler.Location)
	}

	// Node Management API endpoints (Protected by X-Admin-Key)
	apiNodes := r.Group("/api/nodes")
	{
		apiNodes.GET("", nodeHandler.ListNodes)
		apiNodes.GET("/detect", nodeHandler.DetectNode)
		apiNodes.GET("/:id", nodeHandler.GetNode)
		apiNodes.GET("/:id/health", nodeHandler.CheckNodeHealth)

		// Mutation operations strictly protected by X-Admin-Key
		mutationNodes := apiNodes.Group("")
		mutationNodes.Use(middleware.RequireAdminKey(cfg.AdminAPIKey))
		{
			mutationNodes.POST("", nodeHandler.OnboardNode)
			mutationNodes.DELETE("/:id", nodeHandler.RemoveNode)
		}
	}

	serverAddr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("[API] Central API Gin router listening on %s", serverAddr)
	if err := r.Run(serverAddr); err != nil {
		log.Fatalf("[API] Server failed: %v", err)
	}
}

// seedInitialNodes populates initial storage workers into the dynamic registry if empty.
func seedInitialNodes(ctx context.Context, repo repository.NodeRepository, geoSvc *geo.GeoService, initialNodes []models.Replica) {
	count, err := repo.Count(ctx)
	if err != nil || count > 0 {
		return
	}

	log.Printf("[API] Seeding %d default storage nodes into registry...", len(initialNodes))
	for _, n := range initialNodes {
		u, err := url.Parse(n.URL)
		if err != nil {
			continue
		}

		host := u.Hostname()
		port := 22
		if pStr := u.Port(); pStr != "" {
			if p, parseErr := strconv.Atoi(pStr); parseErr == nil {
				port = p
			}
		}

		locLower := strings.ToLower(n.Location)
		coords := geoSvc.StorageLocations[locLower]
		country := strings.ToUpper(n.Location[:1]) + strings.ToLower(n.Location[1:])

		nodeID := fmt.Sprintf("node-%s", locLower)
		now := time.Now().UTC()

		node := &models.StorageNode{
			ID:            uuid.New().String(),
			NodeID:        nodeID,
			IP:            host,
			Port:          port,
			Region:        country,
			Country:       country,
			Latitude:      coords.Latitude,
			Longitude:     coords.Longitude,
			Status:        models.NodeStatusOnline,
			LastHeartbeat: now,
			CreatedAt:     now,
		}

		if err := repo.Create(ctx, node); err != nil {
			log.Printf("[API] Failed to seed node %s: %v", node.NodeID, err)
		} else {
			log.Printf("[API] Seeded node: %s (%s:%d)", node.NodeID, node.IP, node.Port)
		}
	}
}
