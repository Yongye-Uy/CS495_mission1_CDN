package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"mini-cdn/website/internal/config"
	"mini-cdn/website/internal/handlers"
	"mini-cdn/website/internal/middleware"
)

func findDir(candidates ...string) string {
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return candidates[0]
}

func main() {
	cfg := config.Load()

	log.Printf("[WEBSITE] Starting Website Frontend on port %s...", cfg.Port)
	log.Printf("[WEBSITE] Configured API Base URL: %s", cfg.ApiBaseURL)

	// Resolve templates and static directories robustly across Docker & local dev
	templatesDir := findDir("templates", "../templates", "Website/templates", "/app/templates")
	staticDir := findDir("static", "../static", "Website/static", "/app/static")

	log.Printf("[WEBSITE] Resolved templates directory: %s", templatesDir)
	log.Printf("[WEBSITE] Resolved static directory: %s", staticDir)

	webHandler, err := handlers.NewWebHandler(cfg)
	if err != nil {
		log.Fatalf("[WEBSITE] Failed to initialize web handler: %v", err)
	}

	r := gin.Default()
	r.Use(middleware.CSRFMiddleware())

	// Load HTML templates
	pattern := filepath.Join(templatesDir, "*.html")
	r.LoadHTMLGlob(pattern)

	// Static assets
	r.Static("/static", staticDir)

	// Health check
	r.GET("/health", webHandler.HealthCheck)
	r.HEAD("/health", webHandler.HealthCheck)

	// Public Web page routes (support both GET and HEAD)
	r.GET("/", webHandler.Index)
	r.HEAD("/", webHandler.Index)
	r.GET("/files", webHandler.Index)
	r.HEAD("/files", webHandler.Index)
	r.GET("/nodes", webHandler.NodesPage)
	r.HEAD("/nodes", webHandler.NodesPage)

	// Admin Authentication routes
	r.GET("/admin/login", webHandler.LoginPage)
	r.POST("/admin/login", middleware.ValidateCSRF(), webHandler.HandleLogin)
	r.POST("/admin/logout", webHandler.HandleLogout)

	// Admin Protected routes
	admin := r.Group("")
	admin.Use(middleware.RequireAdminSession(cfg))
	{
		admin.GET("/nodes/add", webHandler.AddNodePage)
		admin.HEAD("/nodes/add", webHandler.AddNodePage)
		admin.POST("/nodes/add", middleware.ValidateCSRF(), webHandler.HandleAddNode)
	}

	// Actions
	r.POST("/upload", webHandler.HandleUpload)
	r.POST("/delete/:id", webHandler.HandleDelete)

	// API reverse proxy for any other /api/* requests
	r.Any("/api/*path", webHandler.ProxyAPI)

	serverAddr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("[WEBSITE] Website Gin server listening on %s", serverAddr)
	if err := r.Run(serverAddr); err != nil {
		log.Fatalf("[WEBSITE] Server failed: %v", err)
	}
}
