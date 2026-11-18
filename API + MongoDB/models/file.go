package models

import "time"

// Replica represents a copy of a file residing on a specific storage node.
type Replica struct {
	Location string `json:"location" bson:"location"` // e.g. "singapore", "newyork", "london", "local"
	URL      string `json:"url" bson:"url"`           // Base URL of the storage node
}

// FileMetadata represents the document saved in MongoDB.
type FileMetadata struct {
	ID           string    `json:"id" bson:"_id"`
	OriginalName string    `json:"originalName" bson:"originalName"`
	ContentType  string    `json:"contentType" bson:"contentType"`
	Size         int64     `json:"size" bson:"size"`
	CreatedAt    time.Time `json:"createdAt" bson:"createdAt"`
	Replicas     []Replica `json:"replicas" bson:"replicas"`
}

// ReplicationStatus captures the outcome of replicating a file across storage nodes.
type ReplicationStatus struct {
	Success            bool     `json:"success"`
	Message            string   `json:"message"`
	SuccessfulReplicas []string `json:"successfulReplicas"`
	FailedReplicas     []string `json:"failedReplicas"`
}

// LocationDebugResponse provides diagnostic information regarding nearest server calculation.
type LocationDebugResponse struct {
	FileID            string             `json:"fileId"`
	ClientIP          string             `json:"clientIP"`
	ClientCountry     string             `json:"clientCountry"`
	NearestStorage    string             `json:"nearestStorage"`
	Distances         map[string]float64 `json:"distances"`
	AvailableReplicas []string           `json:"availableReplicas"`
}

// SystemStatus represents the overall health of the distributed system.
type NodeHealth struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	URL      string `json:"url"`
	Status   string `json:"status"` // "ONLINE" or "OFFLINE"
	Error    string `json:"error,omitempty"`
}

type SystemStatusResponse struct {
	API     string       `json:"api"`
	MongoDB string       `json:"mongodb"`
	Storage []NodeHealth `json:"storage"`
}
