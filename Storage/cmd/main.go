package main

import (
	"fmt"
	"log"
	"net/http"

	"mini-cdn/storage/handlers"
	"mini-cdn/storage/internal/config"
	"mini-cdn/storage/services"
	"mini-cdn/storage/storage"
)

func main() {
	cfg := config.Load()

	log.Printf("[STORAGE][%s] Initializing storage node id=%s on port %s (data dir: %s)...", cfg.Location, cfg.NodeID, cfg.Port, cfg.DataDir)

	diskStore, err := storage.NewDiskStorage(cfg.DataDir)
	if err != nil {
		log.Fatalf("[STORAGE][%s] Failed to initialize disk storage: %v", cfg.Location, err)
	}

	storageService := services.NewStorageService(diskStore, cfg.Location, cfg.NodeID)
	handler := handlers.NewStorageHandler(storageService)

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", handler.Health)

	// File operations
	mux.HandleFunc("GET /storage/files", handler.ListFiles)
	mux.HandleFunc("POST /storage/files", handler.Upload)
	mux.HandleFunc("POST /storage/files/{id}", handler.Upload)
	mux.HandleFunc("GET /storage/files/{id}", handler.Get)
	mux.HandleFunc("DELETE /storage/files/{id}", handler.Delete)

	// Sync endpoint for onboarding
	mux.HandleFunc("POST /storage/sync", handler.Sync)

	serverAddr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("[STORAGE][%s] Storage node is listening on %s", cfg.Location, serverAddr)
	if err := http.ListenAndServe(serverAddr, mux); err != nil {
		log.Fatalf("[STORAGE][%s] Server terminated with error: %v", cfg.Location, err)
	}
}
