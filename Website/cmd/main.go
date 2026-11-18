package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"mini-cdn/website/internal/config"
	"mini-cdn/website/internal/handlers"
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

	// Load HTML templates
	pattern := filepath.Join(templatesDir, "*.html")
	r.LoadHTMLGlob(pattern)

	// Static assets
	r.Static("/static", staticDir)

	// Health check
	r.GET("/health", webHandler.HealthCheck)
	r.HEAD("/health", webHandler.HealthCheck)

	// Web page routes (support both GET and HEAD)
	r.GET("/", webHandler.Index)
	r.HEAD("/", webHandler.Index)
	r.GET("/files", webHandler.Index)
	r.HEAD("/files", webHandler.Index)
	r.GET("/nodes", webHandler.NodesPage)
	r.HEAD("/nodes", webHandler.NodesPage)
	r.GET("/nodes/add", webHandler.AddNodePage)
	r.HEAD("/nodes/add", webHandler.AddNodePage)

	// Actions
	r.POST("/nodes/add", webHandler.HandleAddNode)
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
