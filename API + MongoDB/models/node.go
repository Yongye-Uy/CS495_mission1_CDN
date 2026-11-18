package models

import (
	"fmt"
	"time"
)

const (
	NodeStatusPending = "PENDING"
	NodeStatusSyncing = "SYNCING"
	NodeStatusOnline  = "ONLINE"
	NodeStatusOffline = "OFFLINE"
	NodeStatusError   = "ERROR"
)

type StorageNode struct {
	ID            string    `json:"id" bson:"_id"`
	NodeID        string    `json:"nodeId" bson:"node_id"`
	IP            string    `json:"ip" bson:"ip"`
	Region        string    `json:"region" bson:"region"`
	Country       string    `json:"country" bson:"country"`
	Latitude      float64   `json:"latitude" bson:"latitude"`
	Longitude     float64   `json:"longitude" bson:"longitude"`
	Port          int       `json:"port" bson:"port"`
	Status        string    `json:"status" bson:"status"`
	LastHeartbeat time.Time `json:"lastHeartbeat" bson:"last_heartbeat"`
	CreatedAt     time.Time `json:"createdAt" bson:"created_at"`
}

func (n *StorageNode) BaseURL() string {
	if n.Port == 80 || n.Port == 0 {
		return fmt.Sprintf("http://%s", n.IP)
	}
	if n.Port == 443 {
		return fmt.Sprintf("https://%s", n.IP)
	}
	return fmt.Sprintf("http://%s:%d", n.IP, n.Port)
}
